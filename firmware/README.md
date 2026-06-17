# wtwlt firmware

ESP32 firmware for the **wtwlt** weather station: an ESP32 on a **SparkFun
MicroMod Weather Carrier** that samples the sensor suite once a second,
aggregates over 60 s, and publishes readings to MQTT. Lightning strikes are
published as they happen. No vendor cloud — MQTT only.

See [`../SPEC.md`](../SPEC.md) for the full design and the MQTT message contract.

## Hardware

- ESP32 MicroMod Processor + SparkFun MicroMod Weather Carrier
- SparkFun Weather Meter Kit (anemometer, wind vane, rain gauge)
- Analog soil moisture probe (carrier's 3-pin terminal)
- Onboard: BME280 (temp/humidity/pressure), VEML6075 (UV), AS3935 (lightning, SPI)

Pin map lives in [`include/config.h`](include/config.h).

> ⚠️ Two values need confirmation against your board before trusting them:
> the **AS3935 interrupt pin** (`LIGHTNING_INT_PIN`) and the **wind-vane ADC
> table** (`vaneADCValues[]`, bench-calibrated for the ESP32's nonlinear 12-bit
> ADC). See the TODOs in `config.h` / `sensors.cpp`.

## Prerequisites

- [PlatformIO](https://platformio.org/install) — either the VS Code extension
  or the CLI:
  ```bash
  pip install platformio
  ```
- A USB cable to the ESP32. On macOS you may need the CP210x or CH340 USB-serial
  driver depending on the board.

## One-time setup

Copy the secrets template and fill in your WiFi + MQTT details:

```bash
cd firmware
cp include/secrets.example.h include/secrets.h
$EDITOR include/secrets.h
```

`include/secrets.h` is gitignored — it holds WiFi credentials, the MQTT broker
address/login, and this station's `STATION_ID`.

## Build & flash

```bash
cd firmware

pio run                       # compile
pio run --target upload       # compile + flash over USB
pio device monitor            # serial console @ 115200 (Ctrl-C to exit)
```

Or do it in one go:

```bash
pio run --target upload --target monitor
```

In **VS Code + PlatformIO**, use the toolbar: ✔ Build, → Upload, 🔌 Monitor.

### Picking the upload port

PlatformIO usually auto-detects. To list and pin a port:

```bash
pio device list
pio run --target upload --upload-port /dev/cu.usbserial-XXXX
```

(Add `upload_port = ...` / `monitor_port = ...` to `platformio.ini` to make it
permanent.)

### If the board id errors

`platformio.ini` uses the generic `board = esp32dev`, which works for the ESP32
MicroMod Processor. If you've installed the MicroMod board package you may
switch it to `sparkfun_esp32micromod`.

## Verifying it works

After flashing, the serial monitor should show:

```
[wtwlt] booting fw 1.0.0
[net] WiFi connecting....
[net] MQTT connected
[wtwlt] published 60 samples (NNN bytes)   ← once per minute
```

Subscribe on the Pi (or any machine with `mosquitto-clients`) to watch live data:

```bash
mosquitto_sub -h <pi-host> -t 'wtwlt/station/+/#' -v
```

You should see retained `.../status` (online), a `.../readings` message every
~60 s, and `.../lightning` on strikes.

## Project layout

```
firmware/
├── platformio.ini            # board, framework, pinned libraries
├── include/
│   ├── config.h              # pins, cadence, calibration (checked in)
│   ├── secrets.example.h     # credentials template (checked in)
│   └── secrets.h             # your credentials (gitignored)
└── src/
    ├── main.cpp              # scheduler: sample @1 Hz, publish @60 s
    ├── sensors/              # sensor bring-up + reads + lightning
    ├── aggregator.*          # 60 s windowing, vector wind avg, gust, rain delta
    ├── net/                  # WiFi + MQTT + SNTP + retained LWT status
    └── payload.*             # JSON serialization of the MQTT contract
```

## Notes & caveats

- The SparkFun sensor library method names (BME280 / VEML6075 / AS3935) are a
  first-draft best effort; if compilation fails on a sensor call, check the
  installed library's headers and adjust the matching `sensors.cpp` function.
- v1 has **no OTA and no captive portal** — config changes mean a USB reflash.
- During an outage the node reconnects and simply drops readings produced while
  offline (no on-device buffering).
