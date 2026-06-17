// secrets.example.h — template for per-device credentials.
//
//   cp include/secrets.example.h include/secrets.h
//
// Then edit include/secrets.h (gitignored). Changing these requires a USB
// reflash (no captive portal / OTA in v1).
#pragma once

// ---- WiFi ----
#define WIFI_SSID       "your-ssid"
#define WIFI_PASSWORD   "your-wifi-password"

// ---- MQTT broker (Mosquitto on the Pi) ----
#define MQTT_HOST       "192.168.1.10"   // Pi hostname or IP
#define MQTT_PORT       1883
#define MQTT_USER       "wtwlt"          // "" if broker allows anonymous
#define MQTT_PASS       "mqtt-password"  // "" if broker allows anonymous

// ---- Station identity ----
// Used as the MQTT client id and in the <station_id> topic segment / payloads.
#define STATION_ID      "wtwlt-01"
