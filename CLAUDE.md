# CLAUDE.md — working guide for the wtwlt repo

Conventions for working in this codebase. The [top-level README](README.md) is the
canonical reference (architecture, hardware/pin map, MQTT data contract); the
per-directory READMEs cover firmware and server specifics. **Implementation is the
source of truth — there is no separate spec document.**

## What this project is

`wtwlt` ("what's the weather like today") is a home weather station, built as a
**monorepo**:

1. **`firmware/`** — ESP32 on a SparkFun MicroMod Weather Carrier. Samples the
   sensor suite at 1 Hz, aggregates over 60 s, and publishes to MQTT.
2. **`server/`** — a single **Go service** on the Raspberry Pi: MQTT ingest →
   SQLite (with hourly/daily rollups + retention) → dashboard + JSON API.
3. **dashboard** — a self-contained HTML page embedded in the Go binary, served
   at `/`.

Everything is implemented and tested in CI; the remaining work is **hardware
bring-up** on the physical board.

Data path: sensors → ESP32 (1 Hz sample, 60 s aggregate) → **MQTT** → Go ingest →
SQLite → dashboard / API.

## Ground rules

- **The MQTT contract is the integration boundary.** It's documented in the
  top-level README ("MQTT data contract") and implemented in firmware `payload.*`
  + server `internal/model`. If you change a payload field, topic, or unit, update
  all three together (firmware serializer, Go model + tests, README) — they must
  never drift.
- **No vendor cloud.** SparkFun examples push to Arduino IoT Cloud; we don't. The
  node's only network output is MQTT to the Pi.
- **Units: metric/SI on the wire and in storage** (°C, hPa, mm, m/s). Imperial is
  a display-layer conversion (server API `units=` param) — never in firmware or
  the DB.
- **Secrets never get committed.** Firmware credentials live in
  `firmware/include/secrets.h` (gitignored; `secrets.example.h` is the template);
  the server reads a gitignored `.env`.

## Documentation discipline

- **READMEs describe only what the project does *right now*** — never roadmap,
  status, or "planned" features. If a capability isn't built and working, it
  doesn't belong in a README. Update the relevant README (directory-level, and the
  top-level one if behavior/structure changed) with each meaningful change.
- **Implementation is the source of truth.** There is no spec file; README + code
  are canonical. Remaining (hardware) work is tracked in the project memory note,
  not in repo docs.
- Record durable conventions here in `CLAUDE.md`; prune stale guidance.

## Firmware conventions (`firmware/`)

- **Toolchain:** PlatformIO. Build/flash via the `just firmware *` recipes.
- **Layout:** one module per concern — `sensors/`, `aggregator.*`, `net/`,
  `payload.*`, with `main.cpp` as a thin scheduler. Keep each **sensor driver
  isolated** so a single library quirk is easy to fix.
- **Graceful degradation:** a sensor that fails to init should report `null`
  (NAN + have-flag), not crash or emit garbage. The station keeps publishing.
- **Tunables live in `config.h`**, credentials in `secrets.h`. Don't hardcode
  pins/cadence/calibration elsewhere.
- **ESP32 ADC gotchas:** analog reads must use **ADC1 pins** (GPIO 32–39) — ADC2
  is unusable while WiFi is on. Call `analogReadResolution(12)`. The ESP32 ADC is
  nonlinear, so the wind-vane table needs bench calibration.
- **Hardware values to confirm on real hardware** (don't trust the scaffold
  blindly): the AS3935 interrupt pin, the wind-vane `vaneADCValues[]`, and soil
  dry/wet endpoints. Marked `VERIFY`/`TODO` in `config.h`/`sensors.cpp`.
- The firmware **compiles** for `esp32_micromod` against pinned library versions
  (enforced in CI). What remains unverified is on-hardware *behavior*.

## Server conventions (`server/`)

- **One Go binary** does MQTT ingest **and** serves the dashboard/API
  (`eclipse/paho.mqtt.golang` + pure-Go `modernc.org/sqlite` + stdlib `net/http`).
  No separate Python service; the `server/` Python mock publisher is a dev test
  fixture only, never deployed.
- **Database: SQLite** (cgo-free, WAL) embedded in-process. Old data is
  downsampled into `readings_hourly` / `readings_daily` rollup tables on a timer;
  raw is pruned per `WTWLT_RETENTION_DAYS`.
- **Go style:** prefer `samber/lo` for slice transforms (`lo.Map`/`Filter`/etc.)
  over hand-rolled loops; keep classic `for` loops where each iteration needs error
  handling (e.g. `sql.Rows` scanning). Everything stays tested — CI runs
  `go test -race`.
- **Public exposure is already handled** (port-forward + DDNS + reverse proxy on
  the Pi); the service just binds a local port.

## Git

- Work on a branch; open a PR rather than committing to `main` directly.
- Commit/push only when asked.
- Double-check no `secrets.h` / `.env` / DB files are staged before committing.
