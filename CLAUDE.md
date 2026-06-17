# CLAUDE.md — working guide for the wtwlt repo

Context and conventions for working in this codebase. Read alongside
[`SPEC.md`](SPEC.md) (the design + MQTT contract) and the per-directory READMEs.

## What this project is

`wtwlt` ("what's the weather like today") is a home weather station, built as a
**monorepo** with three parts that ship in phases:

1. **`firmware/`** — ESP32 on a SparkFun MicroMod Weather Carrier. Samples the
   sensor suite and publishes to MQTT. **This is the current focus.**
2. **`server/`** (planned) — Raspberry Pi: Mosquitto broker + an ingest service
   that writes to a local DB + an HTTP API.
3. **`web/`** (planned) — public weather dashboard served from the Pi.

Data path: sensors → ESP32 (1 Hz sample, 60 s aggregate) → **MQTT** → Pi ingest
→ DB → web.

## Ground rules

- **The MQTT contract is the integration boundary.** It is defined in
  `SPEC.md §3.3`. If you change a payload field, topic, or unit, update `SPEC.md`
  and the firmware `payload.*` together — they must never drift. The future Pi
  ingest will be written against that spec.
- **No vendor cloud.** SparkFun examples push to Arduino IoT Cloud; we don't.
  The node's only network output is MQTT to the Pi.
- **Units: metric/SI on the wire** (°C, hPa, mm, m/s). Imperial conversion is a
  display concern for the website, not the firmware or DB.
- **Secrets never get committed.** Credentials live in `firmware/include/secrets.h`
  (gitignored); `secrets.example.h` is the checked-in template. Same pattern for
  any future server `.env`.

## Documentation discipline (important)

This is a long, multi-phase build. **Keep the docs current as you implement —
don't batch it up for later:**

- **READMEs describe only what the project does *right now*** — never roadmap,
  status, or "planned" features. If a capability isn't built and working, it
  doesn't belong in a README. After each meaningful change, update the relevant
  `README.md` (the directory one, and the top-level one if behavior/structure
  changed). Treat a feature as unfinished until its README reflects it.
- **Roadmap, phases, and status live in `SPEC.md`** — that's the forward-looking
  document. Keep it honest as phases progress.
- When a design decision changes, record it in `SPEC.md` (decisions table /
  open questions), not just in code comments.
- `SPEC.md` and `CLAUDE.md` are living documents — prune stale guidance.

## Firmware conventions (`firmware/`)

- **Toolchain:** PlatformIO. Build/flash via `pio run` / `pio run -t upload`.
- **Layout:** one module per concern — `sensors/`, `aggregator.*`, `net/`,
  `payload.*`, with `main.cpp` as a thin scheduler. Keep each **sensor driver
  isolated** so a single library quirk is easy to fix.
- **Graceful degradation:** a sensor that fails to init should report `null`
  (NAN + have-flag), not crash or emit garbage. The station keeps publishing.
- **Tunables live in `config.h`**, credentials in `secrets.h`. Don't hardcode
  pins/cadence/calibration elsewhere.
- **ESP32 ADC gotchas:** analog reads must use **ADC1 pins** (GPIO 32–39) —
  ADC2 is unusable while WiFi is on. Call `analogReadResolution(12)`. The ESP32
  ADC is nonlinear, so the wind-vane table needs bench calibration.
- **Hardware values to confirm on real hardware** (don't trust the scaffold
  blindly): the AS3935 interrupt pin, the wind-vane `vaneADCValues[]`, and soil
  dry/wet endpoints. These are marked `VERIFY`/`TODO` in `config.h`/`sensors.cpp`.
- **Verify library APIs against installed versions.** The `SFEWeatherMeterKit`
  API was checked upstream; the BME280/VEML6075/AS3935 calls are best-effort
  until a real `pio run` confirms them.

## Future-phase notes (not yet built)

- **Database is undecided** — see the comparison in `SPEC.md §4` (SQLite is the
  pragmatic lean; RRDtool/DuckDB/VictoriaMetrics are contenders). Optimize for a
  resource-limited Pi: low RAM, SD-card write wear (favor WAL/batched writes).
- **Public exposure is already handled** (port-forward + DDNS + reverse proxy on
  the Pi). The web app only needs to bind a local port.

## Git

- Work on a branch; open a PR rather than committing to `main` directly.
- Commit/push only when asked.
- Double-check no `secrets.h` / `.env` / DB files are staged before committing.
