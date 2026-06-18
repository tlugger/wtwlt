# wtwlt — What's The Weather Like Today

A self-hosted home weather station. An outdoor, solar-powered ESP32 sensor node
publishes readings over WiFi to a Raspberry Pi, which logs them to a local
database and serves a publicly accessible website showing current conditions and
historical trends.

This repo is a **monorepo** containing every part of the system: firmware, the
Pi-side server, and the web frontend.

- **Status:** Phase 1 (firmware) in active design.
- **Last updated:** 2026-06-16

---

## 1. System Overview

```
┌─────────────────────────┐         MQTT/WiFi         ┌──────────────────────────────┐
│  Weather Station Node    │ ───────────────────────▶ │  Raspberry Pi                  │
│  ESP32 + SparkFun        │   (Mosquitto broker on    │                                │
│  MicroMod Weather        │    the Pi)                │  • Mosquitto broker            │
│  Carrier Board           │                           │  • Go service: MQTT ingest →   │
│                          │                           │    local DB → website / API    │
│  Solar + battery, always │                           │    (public exposure already    │
│  awake, publishes 1×/min │                           │     handled by existing        │
│                          │                           │     port-forward + DDNS +       │
│                          │                           │     reverse proxy)             │
└─────────────────────────┘                           └──────────────────────────────┘
```

**Data flow:** Sensors → ESP32 samples @1 Hz → aggregates over 60 s → publishes
one JSON message per minute (plus event-driven lightning messages) to MQTT → a
single **Go service** on the Pi subscribes, persists to the local DB, and serves
the website/API that reads the DB for current + historical views.

---

## 2. Decisions Locked In

These were settled during scoping and drive the design below.

| Area | Decision |
|------|----------|
| Sensor node MCU | ESP32 on a **SparkFun MicroMod Weather Carrier Board** |
| Weather-meter driver | **SparkFun_Weather_Meter_Kit_Arduino_Library** (`SFEWeatherMeterKit`) |
| Cloud services | **None.** No Arduino IoT Cloud / no vendor cloud — collect locally and publish via MQTT only |
| Transport | **MQTT over WiFi**; Mosquitto broker runs on the Pi |
| Power & placement | **Outdoor, solar + battery**, **continuously awake** (no deep sleep) |
| Cadence | Sample **@1 Hz**, publish **aggregated every 60 s** |
| Sensors (v1) | Temp, humidity, pressure (BME280, I²C); **UV index** (VEML6075, I²C); **lightning** (AS3935, **SPI**); wind speed + direction + rain (Weather Meter Kit); soil moisture (**analog** terminal) |
| Units on the wire | **Metric / SI**; website displays both (metric/imperial toggle) |
| Config & updates | **Hardcoded credentials** in a gitignored header; **USB reflash** for changes |
| Outage behavior | **Reconnect & drop gaps** — no on-device buffering in v1 |
| Firmware toolchain | **PlatformIO** |
| Pi backend | **Single Go service** — MQTT ingest + DB writes + website/API in one static binary (low footprint, cgo-free cross-compile to the Pi). Not Python. |
| Dev tooling | `server/` mock publisher stays **Python** — a test fixture only, never deployed |
| Public hosting | **Already set up** (port-forward + DDNS + reverse proxy); web app only needs to bind a local port on the Pi |

---

## 3. Phase 1 — Weather Station Firmware (current focus)

### 3.1 Hardware

- **ESP32** (MicroMod form factor) seated on the **SparkFun MicroMod Weather
  Carrier Board**.
- Onboard sensors used in v1:
  - **BME280** — temperature, relative humidity, barometric pressure (I²C).
  - **VEML6075** — UV-A / UV-B → UV index (I²C).
  - **AS3935** — lightning detection (strike/disturber/noise, distance estimate),
    on **SPI** (CS = GPIO 12), interrupt-driven.
- **SparkFun Weather Meter Kit** (via the carrier's screw terminals / RJ11),
  driven by the **`SFEWeatherMeterKit`** library, which owns the interrupt
  handlers and the vane decode (see §3.4):
  - **Anemometer** — reed-switch pulse; library converts via `kphPerCountPerSec`
    (default **2.4 km/h** per count/sec). `getWindSpeed()` → km/h.
  - **Wind vane** — analog resistive divider on ADC; library maps to **16
    bearings** via the `vaneADCValues[]` table. `getWindDirection()` → degrees.
  - **Rain gauge** — tipping bucket reed switch; library accumulates via
    `mmPerRainfallCount` (default **0.2794 mm/tip**). `getTotalRainfall()` → mm.
- **Soil moisture sensor** on the carrier's **analog terminal** (A0 / GPIO 39,
  ADC1 — WiFi-safe). v1 reports moisture only (raw ADC → % via bench-calibrated
  dry/wet endpoints). If you instead use a Qwiic I²C soil sensor, swap the soil
  module for an I²C driver.
- **Power:** solar panel + LiPo battery with a charge controller. The node runs
  **continuously awake** — sized so the panel/battery survive cloudy stretches.
  Battery voltage is reported in diagnostics so we can monitor the power budget
  in the field.

**Pin map** (SparkFun MicroMod Weather Carrier + ESP32 MicroMod Processor):

| Signal | Pin | Bus / notes |
|--------|-----|-------------|
| Wind direction (vane) | GPIO 35 (A1) | ADC1 — WiFi-safe analog |
| Wind speed (anemometer) | GPIO 23 (D0) | digital, interrupt |
| Rain gauge | GPIO 27 (D1) | digital, interrupt |
| Soil moisture | GPIO 39 (A0) | ADC1 analog terminal |
| AS3935 lightning | CS = GPIO 12 | SPI; INT pin **TBD/verify** |
| BME280 / VEML6075 | I²C | 0x77 / 0x10 |

> Wind and rain are pulse-counted with interrupts (handled inside
> `SFEWeatherMeterKit`). Because the node never deep-sleeps, every gust and
> bucket tip is captured for true per-minute averages and accurate rain
> accumulation.

### 3.2 Sampling & aggregation

- Internal sample loop runs at **1 Hz**.
- A **60 s aggregation window** produces one published "readings" message:
  - **Wind speed:** sample `getWindSpeed()` each second; report the window
    average **and** the peak 1 s reading (gust).
  - **Wind direction:** sample `getWindDirection()` each second and
    vector-average the bearing over the window (so it doesn't flip across the
    0°/360° boundary), plus a cardinal label.
  - **Rain:** read `getTotalRainfall()` (a monotonic accumulator); publish the
    delta over the window. The Pi keeps running daily/period totals. (Avoid
    `resetTotalRainfall()` on the node so a missed message doesn't lose rain.)
  - **Temp / humidity / pressure / UV / soil moisture:** instantaneous read at
    publish time (or windowed mean — TBD, see open questions).
- **Lightning** is **event-driven**, not windowed: each AS3935 interrupt is
  classified and, if it's a real strike, published immediately on its own topic.

### 3.3 MQTT contract

Broker: Mosquitto on the Pi. Suggested QoS 1 for readings/lightning, retained
LWT for status.

**Topics**

| Topic | Purpose | Cadence |
|-------|---------|---------|
| `wtwlt/station/<station_id>/readings` | Aggregated sensor readings | every 60 s |
| `wtwlt/station/<station_id>/lightning` | Lightning strike events | on event |
| `wtwlt/station/<station_id>/status` | Online/offline + identity (LWT, retained) | on connect / disconnect |

**`readings` payload (metric on the wire):**

```json
{
  "station_id": "wtwlt-01",
  "ts": "2026-06-16T12:00:00Z",
  "interval_s": 60,
  "temp_c": 21.4,
  "humidity_pct": 58.2,
  "pressure_hpa": 1013.2,
  "uv_index": 3.1,
  "wind": {
    "avg_mps": 2.4,
    "gust_mps": 5.1,
    "dir_deg": 270,
    "dir_cardinal": "W"
  },
  "rain_mm": 0.5,
  "soil_moisture_pct": 42.0,
  "diagnostics": {
    "battery_v": 3.92,
    "rssi_dbm": -67,
    "uptime_s": 38211,
    "fw_version": "1.0.0"
  }
}
```

**`lightning` payload:**

```json
{
  "station_id": "wtwlt-01",
  "ts": "2026-06-16T12:00:03Z",
  "event": "strike",
  "distance_km": 12,
  "energy": 158473
}
```
`event` ∈ `strike` | `disturber` | `noise` (disturber/noise optionally suppressed).

**`status` payload (retained, LWT flips `online` to false on disconnect):**

```json
{
  "station_id": "wtwlt-01",
  "online": true,
  "fw_version": "1.0.0",
  "ip": "192.168.1.42",
  "boot_ts": "2026-06-16T01:23:45Z"
}
```

> **Time:** The ESP32 syncs the clock via SNTP on boot so `ts` is real UTC. If
> the time sync fails, the Pi stamps arrival time on ingest as a fallback.

### 3.4 Weather Meter Kit driver & calibration

Use **`SFEWeatherMeterKit`** rather than hand-rolling pulse counting. Construct
with the carrier's pins — `SFEWeatherMeterKit(windDirectionPin, windSpeedPin,
rainfallPin)` — call `begin()`, then read with `getWindSpeed()` (km/h),
`getWindDirection()` (deg), `getTotalRainfall()` (mm). The library handles the
wind/rain interrupts and the vane ADC→bearing lookup internally.

Calibration is set via `setCalibrationParams(SFEWeatherMeterKitCalibrationParams)`:

| Field | Default | Notes |
|-------|---------|-------|
| `mmPerRainfallCount` | 0.2794 mm | Rain per bucket tip |
| `kphPerCountPerSec` | 2.4 | Wind speed per count/sec |
| `vaneADCValues[16]` | board default | ADC reading per vane bearing — **must recalibrate for the ESP32 ADC** |
| `windSpeedMeasurementPeriodMillis` | — | Library's internal wind averaging window |
| `minMillisPerRainfall` | — | Debounce between rain counts |

**ESP32 ADC caveats (important):**
- Call `setADCResolutionBits(12)` — ESP32 ADCs are 12-bit (0–4095).
- The ESP32 ADC is **nonlinear** and clamps near its top voltage, so the stock
  `vaneADCValues[]` table will mis-decode directions. Bench-calibrate the table
  by recording the raw ADC at each of the 16 vane positions.

Soil moisture (separate Qwiic sensor, not part of this library): calibrate
raw→% using dry/wet endpoints for the specific probe.

### 3.5 Configuration & secrets

- **`secrets.h`** (gitignored): WiFi SSID/password, MQTT host/port/credentials,
  `station_id`. A checked-in **`secrets.example.h`** documents the fields.
- **`config.h`** (checked in): tunables — sample rate, publish interval,
  calibration constants, lightning sensitivity/noise floor, topic prefix.
- Changes require a **USB reflash** (no captive portal / no OTA in v1).

### 3.6 Resilience

- Auto-reconnect WiFi and MQTT with backoff.
- Readings produced while disconnected are **dropped** (no flash buffering in v1).
- Watchdog timer to recover from hangs.
- LWT marks the station offline so the Pi/dashboard can show staleness.

### 3.7 Firmware project layout

```
firmware/
├── platformio.ini            # board, framework=arduino, lib deps pinned
├── include/
│   ├── config.h              # tunables + calibration (checked in)
│   ├── secrets.h             # gitignored
│   └── secrets.example.h     # template (checked in)
├── src/
│   ├── main.cpp              # setup/loop, scheduler, MQTT lifecycle
│   ├── sensors/              # one module per sensor (bme280, veml6075, as3935,
│   │                         #   wind, rain, soil)
│   ├── aggregator.*          # 60 s windowing, vector wind avg, gust, rain accum
│   ├── net/                  # wifi + mqtt + sntp
│   └── payload.*             # JSON serialization of the MQTT contract
├── lib/
└── test/                     # native unit tests (aggregation, vane decode, JSON)
```

Candidate libraries (to pin in `platformio.ini`): SparkFun BME280, SparkFun
VEML6075, SparkFun AS3935, **SparkFun_Weather_Meter_Kit_Arduino_Library**,
SparkFun Qwiic soil sensor, PubSubClient (or `arduino-mqtt`), ArduinoJson.

> **No vendor cloud.** SparkFun's examples publish to Arduino IoT Cloud; we do
> not use it. The node's only network output is MQTT to the Pi.

### 3.8 Phase 1 — Definition of Done

- [x] PlatformIO project builds for `esp32_micromod` (compile verified + in CI).
- [ ] Firmware flashes and boots on real hardware.
- [ ] All v1 sensors read correctly on the bench with sane values.
- [ ] Wind vane decoded to 16 bearings; anemometer & rain calibrated.
- [ ] AS3935 strikes detected with tuned noise floor.
- [ ] Node connects to WiFi + Mosquitto and publishes valid `readings` JSON
      every 60 s, `lightning` on event, and a retained `status`/LWT.
- [ ] Survives WiFi/broker dropouts and auto-recovers.
- [ ] Diagnostics (battery V, RSSI, uptime, fw version) populated.

---

## 4. Phase 2 — Raspberry Pi Server (future thinking)

Lighter sketch; to be detailed when Phase 1 lands.

- **One Go service.** A single Go binary does ingest **and** serves the website
  (§5) — MQTT subscriber goroutine writes to the DB; HTTP server goroutine reads
  it. One language, one deploy artifact. Proposed stack: `eclipse/paho.mqtt.golang`
  (MQTT client), `modernc.org/sqlite` (pure-Go, cgo-free → trivial
  `GOOS=linux GOARCH=arm64` cross-compile to the Pi), stdlib `net/http`.
- **Broker:** Mosquitto on the Pi (auth + ACLs; optionally TLS on the LAN). A
  local dev broker config + a mock publisher (faithful to §3.3) already exist in
  `server/` for exercising the publish path without hardware. The mock stays
  Python (test fixture only); the deployed service is Go.
- **Ingest (Go):** subscribes to `wtwlt/station/+/readings` and `.../lightning`,
  validates against the schema, and writes to the local DB. Tracks last-seen per
  station for staleness/alerting.
- **API (implemented):** `/api/current`, `/api/history` (raw|hour|day buckets),
  `/api/summary`, `/api/lightning`, `/api/stations`, `/healthz`. Stored metric,
  converted to a `units=metric|imperial` query param at the response layer.
- **Database:** **SQLite** (`modernc.org/sqlite`, cgo-free, WAL) embedded in the
  Go service — see the comparison + rationale below. This workload is small and gentle:
  ~1 write/min (~1,440 rows/day, ~525k rows/year) plus occasional lightning
  events, with reads split between "latest reading" and historical rollups —
  all on a **resource-constrained Pi** (limited RAM, SD-card I/O). The data
  volume is tiny; the real constraints are **memory footprint** and **SD-card
  write wear**, so favor a light engine and batch/WAL writes.

  Options to weigh:

  | Option | Fit | Watch-outs |
  |--------|-----|------------|
  | **SQLite** (pragmatic default) | Tiny footprint, rock-solid, trivially handles 1 write/min; great for "latest" + indexed range reads; ubiquitous tooling | Ad-hoc analytics less ergonomic than columnar, but fine at this volume; use WAL mode |
  | **DuckDB** | Excellent columnar analytics for historical rollups; single file; can query SQLite/Parquet directly | Not built for frequent small concurrent writes; higher RAM during queries on a Pi — batch inserts or use as a read/analytics layer over SQLite |
  | **SQLite + DuckDB hybrid** | Best of both: write to SQLite, let DuckDB query the SQLite file for analytics | Two engines to manage |
  | **RRDtool** | Purpose-built for this exact case: fixed-size round-robin store, automatic downsampling/rollups, very light on a Pi (classic weather-station choice) | Rigid pre-defined schema/retention; lossy consolidation by design; no ad-hoc queries |
  | **VictoriaMetrics** | Lightweight single-binary TSDB, low memory, good on a Pi; built-in retention/downsampling | Another service to run; metrics-model rather than relational |
  | **InfluxDB / TimescaleDB** | Purpose-built time-series with rich queries | Generally **too heavy** for a Pi (InfluxDB RAM use; Postgres footprint) — likely overkill here |

  **Leaning:** the single-Go-binary design points strongly at **SQLite** —
  embedded in-process (no separate service), and `modernc.org/sqlite` keeps the
  build cgo-free for easy ARM cross-compile. Use WAL mode so the ingest goroutine
  writes while the HTTP goroutine reads. Historical analytics can use precomputed
  rollup tables. (RRDtool/VictoriaMetrics are separate services and DuckDB's Go
  driver needs cgo — all cut against the one-binary goal.) Confirm in Phase 2.
- **Retention/rollups:** keep full-resolution recent data; downsample older data
  (hourly/daily aggregates) for "rough historical look."
- **Units:** stored metric; conversions applied at the API/display layer.

---

## 5. Phase 3 — Public Website / Dashboard

> **Status:** v1 dashboard implemented — `server/internal/web/dashboard.html`
> (embedded, served at `/`): earth-tone dynamic theming, "right now" hero,
> range-selectable history charts, summary cards, lightning feed, °C/°F toggle.

- **Hosting/exposure:** already handled — port-forwarding + DDNS + reverse proxy
  are running. The web app only needs to **bind a local port on the Pi**.
- **Architecture:** served by the **same Go binary** (§4) — a self-contained HTML
  page embedded via `go:embed` at `/`, consuming the `/api/*` JSON endpoints.
  Vanilla JS, **no build step**; charts via **Chart.js (CDN)**; mobile-first.
  (Follows the proven `rockiscope` dashboard pattern.)

### Design language

- **Natural / earth tones.** It measures the natural world, so it should feel
  natural: soil/bark browns, moss/sage greens, clay/terracotta, sand/parchment,
  muted sky blues, with a warm ochre/amber accent.
- **Typography:** Raleway (carried from the personal site) for headings; a clean
  readable sans for data.
- **Layout:** Rockiscope's card/panel/grid skeleton, warmed up (earthy surfaces,
  soft borders).
- **Tone:** light personality — clean and informative with the occasional wink
  (tagline, empty states); not a comedy act.

### Dynamic theming (time of day + conditions)

The palette shifts with the local clock **and** the live reading, within the
earthy family:
- **day** → bright warm sky/sand · **dusk** → terracotta/amber/violet · **night**
  → deep soil/indigo.
- condition overlays: **rain** → mossy slate · **storm** (recent lightning) →
  dark slate + lightning-gold accent.
- Condition inferred from available data: recent lightning ⇒ storm; `rain_mm > 0`
  ⇒ rain; else clear. Time of day from the local clock (sunrise/sunset approx).
- Implemented as `[data-theme]` CSS-variable sets switched in JS.

### Content / layout

- **Header:** wordmark + tagline, station online + last-updated, °C/°F toggle
  (persisted in `localStorage`), source link.
- **"Right now" hero:** big current temp + condition glyph, with humidity,
  pressure, wind (avg/gust + direction), UV, soil moisture, rain.
- **Today summary cards:** high/low, total rain, peak gust (`/api/summary`).
- **History charts** (range chips 24h / 7d / 30d → `/api/history`, bucket
  raw/hour/day): temperature, humidity + pressure, wind (avg/gust), rain (bars).
- **Lightning feed:** recent strikes with distance + time; calm-skies empty state.
- **Auto-refresh** current conditions ~every 60 s.
- **Units:** metric default, toggle to imperial (API `units=` param).

---

## 6. Release & Deployment (build LAST — reference plan)

> Deferred until the Go service is functional. Captured here so it isn't lost.
> Pattern proven across other projects (adapted from `tlugger/rockiscope`).

**Goal:** install/update the Pi service with one piped command:

```bash
curl -fsSL https://raw.githubusercontent.com/tlugger/wtwlt/main/install.sh | sudo bash
```

**Release workflow** (`.github/workflows/release.yml`, `workflow_dispatch` with
`branch` + `version` inputs):
- Cross-compile the Go server for `linux/arm64`, `linux/amd64`, `linux/arm`
  (artifacts `wtwlt-server-linux-<arch>`), build from `server/`.
- Emit `sha256` checksums per binary.
- Create/replace the version tag and a GitHub Release with the binaries +
  checksums (`softprops/action-gh-release`, `generate_release_notes`).

**Install script** (`install.sh` at repo root, idempotent installer + updater):
- Detect arch (`uname -m` → arm64/arm/amd64); stop the running service first.
- Download the latest release binary matching the arch; **fallback to building
  from source** (`git clone` + `go build` in `server/`) if no release/binary.
- Install to `/home/pi/wtwlt/wtwlt-server`; create `.env` (MQTT host/port/user/
  pass, HTTP addr) if missing.
- Write a `systemd` unit (`After=network-online.target mosquitto.service`,
  `Restart=always`, `EnvironmentFile=.env`, logs to the install dir),
  `daemon-reload`, `enable`, `(re)start`.
- Re-running upgrades in place (stop → swap binary → restart).
- Nice-to-have UX from the reference: spinner, step/ok/warn/fail helpers, a
  `version` self-check after install.

---

## 7. Open Questions / To Decide Later

- Temp/humidity/pressure/UV/soil: instantaneous-at-publish vs windowed mean?
- Suppress AS3935 `disturber`/`noise` events, or publish them for tuning?
- Bench-calibrate `vaneADCValues[16]` for the ESP32's 12-bit nonlinear ADC.
- Soil moisture raw→% calibration endpoints.
- ~~Phase 2: final DB choice~~ — **decided: SQLite** (`modernc.org/sqlite`,
  cgo-free, WAL) embedded in the Go service. Rollup/downsample strategy for old
  data still TBD.
- Multi-node support later? (`station_id` is already in the contract to allow it.)
- Battery/solar sizing for continuous-awake operation.
```
