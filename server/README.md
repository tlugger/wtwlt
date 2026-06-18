# wtwlt server

Raspberry Pi side of the weather station: a **single Go service** that ingests
the station's MQTT messages into SQLite and serves the website/API from the same
binary. This directory also holds a local Mosquitto config and a Python mock
publisher for exercising the pipeline without hardware.

Message shapes follow the firmware's MQTT contract in [`../SPEC.md`](../SPEC.md)
§3.3 (metric/SI, wind in m/s, NAN→null, retained `status` + LWT).

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

**HTTP endpoints:**

| Route | Returns |
|-------|---------|
| `GET /` | the dashboard (single embedded HTML page) |
| `GET /healthz` | `ok` |
| `GET /api/current` | latest reading for a station |
| `GET /api/history` | time-bucketed aggregates for charts |
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

Deployment (release binaries + a one-line install script with a systemd unit) is
planned for later — see [`../SPEC.md`](../SPEC.md) §6.
