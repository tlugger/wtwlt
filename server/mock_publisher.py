#!/usr/bin/env python3
"""Mock wtwlt weather station.

Publishes readings / lightning / status messages over MQTT so the publish path
(and the future ingest service) can be exercised before the ESP32 hardware
exists. Message shapes mirror the firmware's MQTT contract in SPEC.md §3.3
exactly: metric/SI units, wind in m/s, absent values omitted, a retained status
message, and an LWT that flips the station offline on disconnect.

Usage:
    python mock_publisher.py                 # localhost:1883, station wtwlt-01
    python mock_publisher.py --interval 60   # real firmware cadence
    python mock_publisher.py --help
"""
import argparse
import json
import random
import signal
import time
from datetime import datetime, timezone

import paho.mqtt.client as mqtt

CARDINALS = ["N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE",
             "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"]

FW_VERSION = "mock-1.0.0"


def cardinal(deg: float) -> str:
    return CARDINALS[int((deg + 11.25) % 360 / 22.5)]


def iso_now() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def clamp(v, lo, hi):
    return max(lo, min(hi, v))


class StationState:
    """A gentle random walk so the dashboard shows believable trends."""

    def __init__(self):
        self.temp = 21.0          # degC
        self.humidity = 55.0      # %
        self.pressure = 1013.0    # hPa
        self.uv = 2.0             # index
        self.soil = 40.0          # %
        self.wind = 3.0           # m/s
        self.dir = 180.0          # degrees
        self.batt = 4.05          # volts

    def step(self) -> dict:
        self.temp = clamp(self.temp + random.uniform(-0.3, 0.3), -10, 45)
        self.humidity = clamp(self.humidity + random.uniform(-2, 2), 0, 100)
        self.pressure = clamp(self.pressure + random.uniform(-0.4, 0.4), 950, 1050)
        self.uv = clamp(self.uv + random.uniform(-0.4, 0.4), 0, 12)
        self.soil = clamp(self.soil + random.uniform(-1.5, 1.5), 0, 100)
        self.wind = clamp(self.wind + random.uniform(-0.8, 0.8), 0, 35)
        self.dir = (self.dir + random.uniform(-20, 20)) % 360
        self.batt = clamp(self.batt + random.uniform(-0.02, 0.02), 3.3, 4.2)

        gust = self.wind + random.uniform(0, 4)
        rain = round(random.uniform(0, 0.3), 4) if random.random() < 0.25 else 0.0
        return {"gust": gust, "rain": rain}


def readings_msg(station, state, interval_s, uptime_s) -> dict:
    extra = state.step()
    return {
        "station_id": station,
        "ts": iso_now(),
        "interval_s": interval_s,
        "temp_c": round(state.temp, 1),
        "humidity_pct": round(state.humidity, 1),
        "pressure_hpa": round(state.pressure, 1),
        "uv_index": round(state.uv, 1),
        "wind": {
            "avg_mps": round(state.wind, 1),
            "gust_mps": round(extra["gust"], 1),
            "dir_deg": round(state.dir),
            "dir_cardinal": cardinal(state.dir),
        },
        "rain_mm": extra["rain"],
        "soil_moisture_pct": round(state.soil, 1),
        "diagnostics": {
            "battery_v": round(state.batt, 2),
            "rssi_dbm": random.randint(-80, -55),
            "uptime_s": uptime_s,
            "fw_version": FW_VERSION,
        },
    }


def lightning_msg(station) -> dict:
    return {
        "station_id": station,
        "ts": iso_now(),
        "event": "strike",
        "distance_km": random.randint(1, 40),
        "energy": random.randint(1000, 500000),
    }


def status_msg(station, online, boot_ts) -> dict:
    return {
        "station_id": station,
        "online": online,
        "fw_version": FW_VERSION,
        "ip": "127.0.0.1",
        "boot_ts": boot_ts,
    }


def main():
    ap = argparse.ArgumentParser(description="Mock wtwlt weather station publisher.")
    ap.add_argument("--host", default="localhost")
    ap.add_argument("--port", type=int, default=1883)
    ap.add_argument("--station-id", default="wtwlt-01")
    ap.add_argument("--interval", type=float, default=5.0,
                    help="seconds between readings (firmware uses 60)")
    ap.add_argument("--lightning-prob", type=float, default=0.1,
                    help="probability of a lightning strike per interval")
    ap.add_argument("--username", default=None)
    ap.add_argument("--password", default=None)
    args = ap.parse_args()

    prefix = "wtwlt/station"
    t_readings = f"{prefix}/{args.station_id}/readings"
    t_lightning = f"{prefix}/{args.station_id}/lightning"
    t_status = f"{prefix}/{args.station_id}/status"

    boot_ts = iso_now()
    client = mqtt.Client(mqtt.CallbackAPIVersion.VERSION2, client_id=args.station_id)
    if args.username:
        client.username_pw_set(args.username, args.password)

    # LWT: broker publishes "offline" (retained) if we drop unexpectedly.
    client.will_set(t_status,
                    json.dumps(status_msg(args.station_id, False, boot_ts)),
                    qos=1, retain=True)

    def on_connect(c, userdata, flags, reason_code, properties):
        if reason_code != 0:
            print(f"[mock] connect failed: {reason_code}")
            return
        print(f"[mock] connected to {args.host}:{args.port} as {args.station_id}")
        c.publish(t_status,
                  json.dumps(status_msg(args.station_id, True, boot_ts)),
                  qos=1, retain=True)

    client.on_connect = on_connect
    client.connect(args.host, args.port, keepalive=30)
    client.loop_start()

    running = {"go": True}

    def shutdown(*_):
        running["go"] = False

    signal.signal(signal.SIGINT, shutdown)
    signal.signal(signal.SIGTERM, shutdown)

    state = StationState()
    start = time.monotonic()
    print(f"[mock] publishing every {args.interval}s — Ctrl-C to stop")
    try:
        while running["go"]:
            uptime = int(time.monotonic() - start)
            msg = readings_msg(args.station_id, state, int(args.interval), uptime)
            client.publish(t_readings, json.dumps(msg), qos=1)
            print(f"[mock] readings  temp={msg['temp_c']}C  "
                  f"wind={msg['wind']['avg_mps']}m/s {msg['wind']['dir_cardinal']}  "
                  f"rain={msg['rain_mm']}mm")

            if random.random() < args.lightning_prob:
                lm = lightning_msg(args.station_id)
                client.publish(t_lightning, json.dumps(lm), qos=1)
                print(f"[mock] lightning  {lm['distance_km']}km")

            # responsive sleep so Ctrl-C is snappy
            slept = 0.0
            while running["go"] and slept < args.interval:
                time.sleep(0.1)
                slept += 0.1
    finally:
        # graceful offline status (in addition to the LWT)
        client.publish(t_status,
                       json.dumps(status_msg(args.station_id, False, boot_ts)),
                       qos=1, retain=True)
        client.loop_stop()
        client.disconnect()
        print("\n[mock] stopped")


if __name__ == "__main__":
    main()
