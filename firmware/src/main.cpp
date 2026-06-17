// main.cpp — wtwlt weather station entry point.
//
// Flow (see SPEC.md §3): sample sensors @1 Hz, aggregate over 60 s, publish one
// readings message per window over MQTT. Lightning is event-driven. The node
// stays awake continuously (solar + battery) and reconnects on WiFi/MQTT drops.
#include <Arduino.h>

#include "config.h"
#include "sensors/sensors.h"
#include "aggregator.h"
#include "payload.h"
#include "net/wifi_mqtt.h"

namespace {

uint32_t lastSample  = 0;
uint32_t lastPublish = 0;
float    lastRainTotal = 0;

float readBatteryVolts() {
  if (BATTERY_ADC_PIN == 0xFF) return NAN;  // disabled
  int raw = analogRead(BATTERY_ADC_PIN);
  return (raw * ADC_REF_VOLTS / ADC_MAX_COUNTS) * BATTERY_DIVIDER;
}

}  // namespace

void setup() {
  Serial.begin(115200);
  delay(200);
  Serial.println("\n[wtwlt] booting fw " FW_VERSION);

  sensors::begin();
  net::begin();

  agg::reset(0);
  uint32_t now = millis();
  lastSample = now;
  lastPublish = now;

  // Seed the rain baseline from the current accumulator.
  SensorReading first = sensors::read();
  lastRainTotal = first.totalRainMm;
  agg::reset(lastRainTotal);
}

void loop() {
  net::loop();

  // --- Lightning: event-driven ---
  LightningEvent ev;
  if (sensors::pollLightning(ev)) {
    char buf[192];
    char ts[32];
    payload::isoNow(ts, sizeof ts);
    size_t n = payload::buildLightning(buf, sizeof buf, ev, ts);
    net::publishLightning(buf, n);
    Serial.printf("[wtwlt] lightning: %s @ %u km\n", ev.type, ev.distanceKm);
  }

  uint32_t now = millis();

  // --- 1 Hz sample ---
  if (now - lastSample >= SAMPLE_INTERVAL_MS) {
    lastSample += SAMPLE_INTERVAL_MS;
    SensorReading r = sensors::read();
    agg::addSample(r);
    lastRainTotal = r.totalRainMm;
  }

  // --- 60 s aggregated publish ---
  if (now - lastPublish >= PUBLISH_INTERVAL_MS) {
    lastPublish += PUBLISH_INTERVAL_MS;

    agg::Window w = agg::finalize(lastRainTotal);

    Diagnostics diag;
    diag.batteryV = readBatteryVolts();
    diag.rssiDbm  = net::rssiDbm();
    diag.uptimeS  = millis() / 1000;

    char buf[MQTT_BUFFER_SIZE];
    char ts[32];
    payload::isoNow(ts, sizeof ts);
    size_t n = payload::buildReadings(buf, sizeof buf, w, diag, ts);
    net::publishReadings(buf, n);
    Serial.printf("[wtwlt] published %u samples (%u bytes)\n", w.samples, (unsigned)n);

    agg::reset(lastRainTotal);
  }
}
