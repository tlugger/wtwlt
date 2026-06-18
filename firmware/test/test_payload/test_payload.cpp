// Host-based tests for the MQTT JSON contract (see the README).
// Builds each message, parses it back, and asserts field names / units / nulls.
// Run with:  just firmware test   (or  pio test -e native)
#include <unity.h>
#include <ArduinoJson.h>
#include <math.h>
#include <string.h>
#include "payload.h"
#include "config.h"

void setUp() {}
void tearDown() {}

// --- readings: fully-populated window ---
void test_readings_full() {
  agg::Window w{};
  w.haveBME = true;  w.tempC = 21.4f; w.humidityPct = 58.2f; w.pressureHpa = 1013.2f;
  w.haveUV = true;   w.uvIndex = 3.1f;
  w.haveSoil = true; w.soilPct = 42.0f;
  w.windAvgKph = 7.2f; w.windGustKph = 18.0f; w.windDirDeg = 270.0f; w.windCardinal = "W";
  w.rainMm = 0.5f;   w.samples = 60;

  Diagnostics d{}; d.batteryV = 3.92f; d.rssiDbm = -67; d.uptimeS = 38211;

  char buf[512];
  size_t n = payload::buildReadings(buf, sizeof buf, w, d, "2026-06-16T12:00:00Z");
  TEST_ASSERT_GREATER_THAN(0, n);

  JsonDocument doc;
  TEST_ASSERT_FALSE(deserializeJson(doc, buf));

  // identity / framing
  TEST_ASSERT_TRUE(doc["station_id"].is<const char*>());
  TEST_ASSERT_TRUE(strlen(doc["station_id"].as<const char*>()) > 0);
  TEST_ASSERT_EQUAL_STRING("2026-06-16T12:00:00Z", doc["ts"].as<const char*>());
  TEST_ASSERT_EQUAL_INT(60, doc["interval_s"].as<int>());

  // scalar sensors
  TEST_ASSERT_FLOAT_WITHIN(0.01f, 21.4f, doc["temp_c"].as<float>());
  TEST_ASSERT_FLOAT_WITHIN(0.01f, 58.2f, doc["humidity_pct"].as<float>());
  TEST_ASSERT_FLOAT_WITHIN(0.01f, 1013.2f, doc["pressure_hpa"].as<float>());
  TEST_ASSERT_FLOAT_WITHIN(0.01f, 3.1f, doc["uv_index"].as<float>());
  TEST_ASSERT_FLOAT_WITHIN(0.01f, 42.0f, doc["soil_moisture_pct"].as<float>());

  // wind: km/h -> m/s on the wire
  TEST_ASSERT_FLOAT_WITHIN(0.001f, 7.2f / 3.6f, doc["wind"]["avg_mps"].as<float>());
  TEST_ASSERT_FLOAT_WITHIN(0.001f, 18.0f / 3.6f, doc["wind"]["gust_mps"].as<float>());
  TEST_ASSERT_FLOAT_WITHIN(0.01f, 270.0f, doc["wind"]["dir_deg"].as<float>());
  TEST_ASSERT_EQUAL_STRING("W", doc["wind"]["dir_cardinal"].as<const char*>());

  // rain
  TEST_ASSERT_FLOAT_WITHIN(0.001f, 0.5f, doc["rain_mm"].as<float>());

  // diagnostics
  TEST_ASSERT_FLOAT_WITHIN(0.01f, 3.92f, doc["diagnostics"]["battery_v"].as<float>());
  TEST_ASSERT_EQUAL_INT(-67, doc["diagnostics"]["rssi_dbm"].as<int>());
  TEST_ASSERT_EQUAL_UINT32(38211, doc["diagnostics"]["uptime_s"].as<uint32_t>());
  TEST_ASSERT_EQUAL_STRING(FW_VERSION, doc["diagnostics"]["fw_version"].as<const char*>());
}

// --- readings: absent sensors and battery emit JSON null, not garbage ---
void test_readings_nulls() {
  agg::Window w{};               // all have-flags false, scalars NAN by default
  w.windCardinal = "N";
  Diagnostics d{}; d.batteryV = NAN; d.rssiDbm = 0; d.uptimeS = 0;

  char buf[512];
  payload::buildReadings(buf, sizeof buf, w, d, "2026-06-16T12:00:00Z");

  JsonDocument doc;
  TEST_ASSERT_FALSE(deserializeJson(doc, buf));

  TEST_ASSERT_TRUE(doc["temp_c"].isNull());
  TEST_ASSERT_TRUE(doc["humidity_pct"].isNull());
  TEST_ASSERT_TRUE(doc["pressure_hpa"].isNull());
  TEST_ASSERT_TRUE(doc["uv_index"].isNull());
  TEST_ASSERT_TRUE(doc["soil_moisture_pct"].isNull());
  TEST_ASSERT_TRUE(doc["diagnostics"]["battery_v"].isNull());
  // wind is always present even with no samples
  TEST_ASSERT_FALSE(doc["wind"]["avg_mps"].isNull());
}

// --- lightning event ---
void test_lightning() {
  LightningEvent ev; ev.type = "strike"; ev.distanceKm = 12; ev.energy = 158473;

  char buf[192];
  payload::buildLightning(buf, sizeof buf, ev, "2026-06-16T12:00:03Z");

  JsonDocument doc;
  TEST_ASSERT_FALSE(deserializeJson(doc, buf));
  TEST_ASSERT_EQUAL_STRING("2026-06-16T12:00:03Z", doc["ts"].as<const char*>());
  TEST_ASSERT_EQUAL_STRING("strike", doc["event"].as<const char*>());
  TEST_ASSERT_EQUAL_INT(12, doc["distance_km"].as<int>());
  TEST_ASSERT_EQUAL_UINT32(158473, doc["energy"].as<uint32_t>());
}

// --- status / LWT ---
void test_status() {
  char buf[200];
  payload::buildStatus(buf, sizeof buf, /*online=*/true, "192.168.1.42",
                       "2026-06-16T01:23:45Z");

  JsonDocument doc;
  TEST_ASSERT_FALSE(deserializeJson(doc, buf));
  TEST_ASSERT_TRUE(doc["online"].as<bool>());
  TEST_ASSERT_EQUAL_STRING("192.168.1.42", doc["ip"].as<const char*>());
  TEST_ASSERT_EQUAL_STRING(FW_VERSION, doc["fw_version"].as<const char*>());
  TEST_ASSERT_EQUAL_STRING("2026-06-16T01:23:45Z", doc["boot_ts"].as<const char*>());
}

int runUnityTests() {
  UNITY_BEGIN();
  RUN_TEST(test_readings_full);
  RUN_TEST(test_readings_nulls);
  RUN_TEST(test_lightning);
  RUN_TEST(test_status);
  return UNITY_END();
}

#if defined(ARDUINO)
#include <Arduino.h>
void setup() { runUnityTests(); }
void loop() {}
#else
int main() { return runUnityTests(); }
#endif
