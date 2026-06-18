# wtwlt — What's The Weather Like Today

A self-hosted home weather station, built as a **monorepo**.

Currently the repo contains:

- **`firmware/`** — ESP32 weather-station firmware. It runs on an ESP32 mounted on
  a SparkFun MicroMod Weather Carrier, samples the sensor suite (temperature,
  humidity, barometric pressure, UV index, lightning, wind speed/direction, rain,
  and soil moisture) once a second, aggregates over a 60-second window, and
  publishes readings to an MQTT broker. Lightning strikes are published as they
  happen. Units on the wire are metric/SI.
- **`server/`** — the Raspberry Pi backend: a single **Go service** that ingests
  the station's MQTT messages into SQLite and serves a JSON API from the same
  binary. Ships with a local Mosquitto config and a Python mock publisher so the
  whole pipeline can be exercised before the hardware exists.

For the full system design — including the planned Raspberry Pi ingest, database,
and public website — see [`SPEC.md`](SPEC.md).

## Build & flash the firmware

This repo uses [`just`](https://github.com/casey/just) as a task runner. Firmware
recipes are namespaced under `firmware`:

```bash
just firmware secrets   # create firmware/include/secrets.h from the template, then edit it
just firmware test      # host unit tests (no board needed)
just firmware build     # compile for the ESP32
just firmware flash     # flash over USB  (append /dev/cu.usbserial-XXXX for a specific port)
just firmware dev       # flash, then open the serial monitor
```

Run `just` with no arguments to list recipes and modules. Full instructions, the
pin map, and verification steps are in [`firmware/README.md`](firmware/README.md).

## Run the server pipeline (no hardware)

Exercise MQTT → Go ingest → SQLite → API end-to-end (requires `brew install mosquitto`):

```bash
just server broker   # terminal 1: start Mosquitto
just server run      # terminal 2: start the Go service (ingests -> SQLite, serves API)
just server setup    # terminal 3: one-time, create the mock venv
just server mock     # terminal 3: publish mock readings/lightning/status

curl localhost:8080/api/current   # latest reading
```

Details in [`server/README.md`](server/README.md).
