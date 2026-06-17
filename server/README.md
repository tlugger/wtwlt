# wtwlt server

Raspberry Pi side of the weather station. Today this contains a local
**Mosquitto broker config** and a **mock publisher** so the MQTT publish path can
be exercised end-to-end before the ESP32 hardware exists. (Ingest + database +
the public website come later — see [`../SPEC.md`](../SPEC.md) §4–5.)

The mock publisher emits the exact message contract the firmware uses
([`../SPEC.md`](../SPEC.md) §3.3): metric/SI units, wind in m/s, a retained
`status` message, and an LWT that flips the station offline on disconnect.

## Prerequisites

- **Mosquitto** (broker + `mosquitto_sub`/`mosquitto_pub` clients):
  ```bash
  brew install mosquitto
  ```
- **Python 3** (for the mock publisher; uses a local venv).

## Quick start

Run each in its own terminal, from the repo root:

```bash
just server setup      # one-time: create .venv and install paho-mqtt
just server broker     # terminal 1: start Mosquitto (Ctrl-C to stop)
just server watch      # terminal 2: subscribe to wtwlt/#  (live message feed)
just server mock       # terminal 3: publish mock readings/lightning/status
```

You should see a retained `.../status` message (online), a `.../readings`
message every few seconds, and occasional `.../lightning` events in the `watch`
terminal.

Useful options on the publisher:

```bash
just server mock --interval 60          # real firmware cadence (60 s)
just server mock --station-id wtwlt-02  # simulate a second station
just server mock --lightning-prob 0.5   # more frequent strikes for testing
```

## Files

- `mosquitto/mosquitto.conf` — dev broker config (anonymous, port 1883).
- `mock_publisher.py` — faithful stand-in for the ESP32 firmware.
- `requirements.txt` — Python deps (paho-mqtt).

## Production note

The dev config allows anonymous connections for convenience. On the real Pi,
disable anonymous access and require credentials:

```conf
allow_anonymous false
password_file /etc/mosquitto/passwd
```

Create the password file with `mosquitto_passwd -c /etc/mosquitto/passwd wtwlt`,
and set the matching `MQTT_USER`/`MQTT_PASS` in the firmware's `secrets.h`.
