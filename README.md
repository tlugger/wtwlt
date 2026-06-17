# wtwlt — What's The Weather Like Today

A self-hosted home weather station, built as a **monorepo**.

Currently the repo contains the **ESP32 weather-station firmware**: it runs on an
ESP32 mounted on a SparkFun MicroMod Weather Carrier, samples the sensor suite
(temperature, humidity, barometric pressure, UV index, lightning, wind
speed/direction, rain, and soil moisture) once a second, aggregates over a
60-second window, and publishes readings to an MQTT broker. Lightning strikes
are published as they happen. Units on the wire are metric/SI.

For the full system design — including the planned Raspberry Pi server and
public website — see [`SPEC.md`](SPEC.md).

## Build & flash the firmware

```bash
cd firmware
cp include/secrets.example.h include/secrets.h   # then edit with your WiFi/MQTT
pio run --target upload --target monitor
```

Full instructions, the pin map, and verification steps are in
[`firmware/README.md`](firmware/README.md).
