# wtwlt server

Raspberry Pi side of the weather station: a **single Go service** that ingests
the station's MQTT messages into SQLite and serves the website/API from the same
binary. This directory also holds a local Mosquitto config and a Python mock
publisher for exercising the pipeline without hardware.

Message shapes follow the firmware's MQTT contract in the
[top-level README](../README.md#mqtt-data-contract) (metric/SI, wind in m/s,
NAN→null, retained `status` + LWT).

## The Go service

```bash
just server build    # compile -> server/wtwlt-server
just server run      # run locally (reads WTWLT_* env)
just server test     # Go unit tests (model parsing + SQLite store)
just server vet      # go vet
```

It runs two things in one process: an MQTT subscriber goroutine that writes to
SQLite (WAL mode), and an HTTP server goroutine that reads from it.

**Config** (environment variables, with defaults):

| Var | Default | Purpose |
|-----|---------|---------|
| `WTWLT_MQTT_HOST` | `localhost` | broker host |
| `WTWLT_MQTT_PORT` | `1883` | broker port |
| `WTWLT_MQTT_USER` / `WTWLT_MQTT_PASS` | _(empty)_ | broker auth (if enabled) |
| `WTWLT_HTTP_ADDR` | `:8080` | HTTP listen address |
| `WTWLT_DB_PATH` | `./wtwlt.db` | SQLite file (or `$WTWLT_DATA_DIR/wtwlt.db`) |
| `WTWLT_RETENTION_DAYS` | `90` | prune raw readings older than this (`0` = keep all) |
| `WTWLT_FORECAST_PROVIDER` | `openmeteo` | forecast source: `openmeteo` \| `nws` \| `none` |
| `WTWLT_LAT` / `WTWLT_LON` | `39.7392` / `-104.9903` | station coordinates (forecast location) |
| `WTWLT_FORECAST_MINUTES` | `60` | forecast poll interval |

**Forecast overlay:** on a timer the service fetches a near-term hourly forecast
from a keyless provider and stores it (in metric/SI) in a separate `forecast`
table — sensor tables hold measured data only. The dashboard draws it as a
dashed, muted projection continuing each chart past "now". Providers are
swappable: **Open-Meteo** (default; one keyless call, covers every field
including pressure) or **NWS/NOAA** (official US source, also keyless, but its
hourly product carries no barometric pressure or precip amount — those publish
as `null`). Set `WTWLT_FORECAST_PROVIDER=none` to disable. A normalized
condition (clear/cloudy/rain/…) is derived per hour and drives the dashboard's
forecast tiles (4-hour segments on the 24h view, daily otherwise).

**Rollups & retention:** every 10 minutes the service recomputes hourly/daily
aggregates from raw into `readings_hourly` / `readings_daily`, then prunes raw
older than `WTWLT_RETENTION_DAYS`. The `/api/history` `hour` and `day` buckets
read these rollup tables, so long-range charts stay fast and survive pruning
(`raw` reads live readings). Keep retention ≥ your longest raw-resolution view.

**HTTP endpoints:**

| Route | Returns |
|-------|---------|
| `GET /` | the dashboard (single embedded HTML page) |
| `GET /healthz` | `ok` |
| `GET /api/current` | latest reading for a station |
| `GET /api/history` | time-bucketed aggregates for charts |
| `GET /api/forecast` | hourly forecast for the coming week (chart overlay + tiles) |
| `GET /api/summary` | min/max/avg + total rain over a range |
| `GET /api/lightning` | recent strike events (newest first) |
| `GET /api/stations` | status (online/offline, last-seen) of all stations |

Common query params:

- `station` — station id (default `wtwlt-01`).
- `units` — `metric` (default) or `imperial`. Values are stored metric and
  converted at the API layer; responses include a `units` descriptor object and a
  `unit_system` field. Dashboard endpoints use unit-neutral field names
  (`temp`, `wind_avg`, `rain`, …) so the toggle is unambiguous.
- `from` / `to` — RFC3339 timestamps (default: last 24h).
- `bucket` (history) — `raw` | `hour` | `day` (history aggregation granularity).
- `limit` (lightning) — max events, default 100, capped at 1000.

Example:

```bash
curl 'localhost:8080/api/history?bucket=hour&units=imperial&from=2026-06-16T00:00:00Z&to=2026-06-17T00:00:00Z'
curl 'localhost:8080/api/summary?units=imperial'
```

**DB:** SQLite via `modernc.org/sqlite` (pure Go, cgo-free) — so it cross-compiles
to the Pi with a plain `GOOS=linux GOARCH=arm64 go build`.

## Prerequisites

- **Go** (toolchain version is pinned in `go.mod`).
- **Mosquitto** (broker + clients) for the local broker/mock:
  `brew install mosquitto`.
- **Python 3** for the mock publisher (uses a local venv).

## Exercise the full pipeline (no hardware)

Run each in its own terminal, from the repo root:

```bash
just server broker     # terminal 1: start Mosquitto
just server run        # terminal 2: start the Go service (ingests -> SQLite)
just server setup      # terminal 3: one-time, create mock venv
just server mock       # terminal 3: publish mock readings/lightning/status
```

Then open the **dashboard** at <http://localhost:8080/> — an earth-toned page
whose theme shifts with time of day and live conditions (clear/dusk/night/rain/
storm). Or query the API directly:

```bash
curl localhost:8080/api/current
curl localhost:8080/api/stations
```

`just server watch` (a raw `mosquitto_sub` on `wtwlt/#`) is handy for seeing the
bus traffic directly.

Useful mock options:

```bash
just server mock --interval 60          # real firmware cadence (60 s)
just server mock --station-id wtwlt-02  # simulate a second station
just server mock --lightning-prob 0.5   # more frequent strikes for testing
```

## Files

- `main.go`, `internal/` — the Go service (config, model, store, ingest, web).
- `mosquitto/mosquitto.conf` — dev broker config (anonymous, port 1883).
- `mock_publisher.py`, `requirements.txt` — Python dev test fixture (not deployed).

## Production note

The dev broker config allows anonymous connections for convenience. On the real
Pi, disable anonymous access and require credentials:

```conf
allow_anonymous false
password_file /etc/mosquitto/passwd
```

Create the password file with `mosquitto_passwd -c /etc/mosquitto/passwd wtwlt`,
set the matching `MQTT_USER`/`MQTT_PASS` in the firmware's `secrets.h`, and the
matching `WTWLT_MQTT_USER`/`WTWLT_MQTT_PASS` for the Go service.

## Deployment

Release binaries are cross-compiled by the GitHub **release** workflow
(`.github/workflows/release.yml`, run manually with a version tag). On the Pi,
install/update with one command:

```bash
curl -fsSL https://raw.githubusercontent.com/tlugger/wtwlt/main/install.sh | sudo bash
```

`install.sh` downloads the right binary for the Pi's architecture (or builds from
source as a fallback), provisions the **Mosquitto broker** (installs it if
missing and writes a port-1883 listener conf — anonymous, or auth matching the
`.env` `WTWLT_MQTT_USER`/`WTWLT_MQTT_PASS` if set), writes `/home/pi/wtwlt/.env`
and a `systemd` unit, and starts the `wtwlt` service. Re-running upgrades in
place. Set `WTWLT_SKIP_BROKER=1` to manage the broker yourself.

If the broker uses auth, set the **firmware's** `secrets.h` `MQTT_USER`/`MQTT_PASS`
to the same credentials so the station can connect.
