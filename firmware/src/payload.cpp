// payload.cpp — builds the JSON messages defined in SPEC.md §3.3.
#include "payload.h"
#include "config.h"
#include "secrets.h"

#include <ArduinoJson.h>
#include <time.h>
#include <math.h>

namespace {
// Add a float field, or null when the value is NAN.
void addNum(JsonObject obj, const char *key, float v) {
  if (isnan(v)) obj[key] = nullptr;
  else          obj[key] = v;
}
}  // namespace

namespace payload {

void isoNow(char *buf, size_t len) {
  time_t now = time(nullptr);
  if (now < 1700000000) {  // SNTP not synced yet
    snprintf(buf, len, "1970-01-01T00:00:00Z");
    return;
  }
  struct tm tm_utc;
  gmtime_r(&now, &tm_utc);
  strftime(buf, len, "%Y-%m-%dT%H:%M:%SZ", &tm_utc);
}

size_t buildReadings(char *buf, size_t len, const agg::Window &w,
                     const Diagnostics &diag, const char *tsIso) {
  JsonDocument doc;
  JsonObject root = doc.to<JsonObject>();  // grab the root once
  root["station_id"] = STATION_ID;
  root["ts"]         = tsIso;
  root["interval_s"] = PUBLISH_INTERVAL_MS / 1000;

  addNum(root, "temp_c",       w.haveBME ? w.tempC : NAN);
  addNum(root, "humidity_pct", w.haveBME ? w.humidityPct : NAN);
  addNum(root, "pressure_hpa", w.haveBME ? w.pressureHpa : NAN);
  addNum(root, "uv_index",     w.haveUV ? w.uvIndex : NAN);

  // km/h -> m/s for the wire (SI)
  JsonObject wind = root["wind"].to<JsonObject>();
  wind["avg_mps"]      = w.windAvgKph / 3.6f;
  wind["gust_mps"]     = w.windGustKph / 3.6f;
  wind["dir_deg"]      = w.windDirDeg;
  wind["dir_cardinal"] = w.windCardinal;

  root["rain_mm"] = w.rainMm;
  addNum(root, "soil_moisture_pct", w.haveSoil ? w.soilPct : NAN);

  JsonObject d = root["diagnostics"].to<JsonObject>();
  addNum(d, "battery_v", diag.batteryV);
  d["rssi_dbm"]   = diag.rssiDbm;
  d["uptime_s"]   = diag.uptimeS;
  d["fw_version"] = FW_VERSION;

  return serializeJson(doc, buf, len);
}

size_t buildLightning(char *buf, size_t len, const LightningEvent &ev,
                      const char *tsIso) {
  JsonDocument doc;
  doc["station_id"]  = STATION_ID;
  doc["ts"]          = tsIso;
  doc["event"]       = ev.type;
  doc["distance_km"] = ev.distanceKm;
  doc["energy"]      = ev.energy;
  return serializeJson(doc, buf, len);
}

size_t buildStatus(char *buf, size_t len, bool online, const char *ip,
                   const char *tsIso) {
  JsonDocument doc;
  doc["station_id"] = STATION_ID;
  doc["online"]     = online;
  doc["fw_version"] = FW_VERSION;
  doc["ip"]         = ip;
  doc["boot_ts"]    = tsIso;
  return serializeJson(doc, buf, len);
}

}  // namespace payload
