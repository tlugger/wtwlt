// wifi_mqtt.cpp — WiFi + MQTT with auto-reconnect and a retained LWT status.
#include "net/wifi_mqtt.h"
#include "config.h"
#include "secrets.h"
#include "payload.h"

#include <WiFi.h>
#include <PubSubClient.h>
#include <time.h>

namespace {

WiFiClient   wifiClient;
PubSubClient mqtt(wifiClient);

char topicReadings[96];
char topicLightning[96];
char topicStatus[96];

uint32_t lastReconnectAttempt = 0;
const uint32_t RECONNECT_BACKOFF_MS = 5000;

// boot_ts handling: the clock isn't set until SNTP syncs (a few seconds after
// WiFi), which is usually after the first MQTT connect. We cache the real boot
// time once the clock is valid (now - uptime) and republish status with it.
char g_bootIso[32] = "";        // real boot timestamp, empty until SNTP syncs
bool g_statusFinalized = false; // republished status with the real boot_ts yet?

bool timeSynced() { return time(nullptr) > 1700000000; }

void updateBootIso() {
  if (g_bootIso[0] != '\0' || !timeSynced()) return;
  time_t boot = time(nullptr) - (time_t)(millis() / 1000);
  struct tm tm_utc;
  gmtime_r(&boot, &tm_utc);
  strftime(g_bootIso, sizeof g_bootIso, "%Y-%m-%dT%H:%M:%SZ", &tm_utc);
}

// boot_ts to advertise: the cached real value, or an epoch fallback pre-sync.
const char *bootTs(char *buf, size_t len) {
  if (g_bootIso[0] != '\0') return g_bootIso;
  payload::isoNow(buf, len);
  return buf;
}

void publishStatus(bool online) {
  char msg[160], tsbuf[32];
  size_t n = payload::buildStatus(msg, sizeof msg, online,
                                  WiFi.localIP().toString().c_str(),
                                  bootTs(tsbuf, sizeof tsbuf));
  mqtt.publish(topicStatus, (const uint8_t *)msg, n, /*retain=*/true);
}

void buildTopics() {
  snprintf(topicReadings,  sizeof topicReadings,  "%s/%s/readings",  TOPIC_PREFIX, STATION_ID);
  snprintf(topicLightning, sizeof topicLightning, "%s/%s/lightning", TOPIC_PREFIX, STATION_ID);
  snprintf(topicStatus,    sizeof topicStatus,    "%s/%s/status",    TOPIC_PREFIX, STATION_ID);
}

void ensureWifi() {
  if (WiFi.status() == WL_CONNECTED) return;
  WiFi.mode(WIFI_STA);
  WiFi.begin(WIFI_SSID, WIFI_PASSWORD);
}

bool mqttConnect() {
  if (WiFi.status() != WL_CONNECTED) return false;
  updateBootIso();

  // LWT: retained "offline" status published by the broker if we drop.
  char willMsg[160], tsbuf[32];
  payload::buildStatus(willMsg, sizeof willMsg, /*online=*/false,
                       WiFi.localIP().toString().c_str(), bootTs(tsbuf, sizeof tsbuf));

  bool ok = mqtt.connect(STATION_ID, MQTT_USER, MQTT_PASS,
                         topicStatus, MQTT_QOS, /*retain=*/true, willMsg);
  if (ok) {
    publishStatus(/*online=*/true);  // retained, so subscribers see current state
    Serial.println("[net] MQTT connected");
  } else {
    Serial.printf("[net] MQTT connect failed, state=%d\n", mqtt.state());
  }
  return ok;
}

}  // namespace

namespace net {

void begin() {
  buildTopics();

  WiFi.mode(WIFI_STA);
  WiFi.begin(WIFI_SSID, WIFI_PASSWORD);
  Serial.print("[net] WiFi connecting");
  uint32_t start = millis();
  while (WiFi.status() != WL_CONNECTED && millis() - start < 20000) {
    delay(250);
    Serial.print(".");
  }
  Serial.println();

  configTime(0, 0, NTP_SERVER);  // UTC

  mqtt.setServer(MQTT_HOST, MQTT_PORT);
  mqtt.setBufferSize(MQTT_BUFFER_SIZE);
  mqttConnect();
}

void loop() {
  ensureWifi();

  if (!mqtt.connected()) {
    uint32_t now = millis();
    if (now - lastReconnectAttempt >= RECONNECT_BACKOFF_MS) {
      lastReconnectAttempt = now;
      mqttConnect();
    }
  } else {
    mqtt.loop();
    // Once SNTP syncs after an early connect, republish status with the real
    // boot_ts (the first publish used an epoch fallback).
    updateBootIso();
    if (g_bootIso[0] != '\0' && !g_statusFinalized) {
      publishStatus(/*online=*/true);
      g_statusFinalized = true;
    }
  }
}

bool connected() { return mqtt.connected(); }

void publishReadings(const char *json, size_t n) {
  if (mqtt.connected())
    mqtt.publish(topicReadings, (const uint8_t *)json, n, /*retain=*/false);
}

void publishLightning(const char *json, size_t n) {
  if (mqtt.connected())
    mqtt.publish(topicLightning, (const uint8_t *)json, n, /*retain=*/false);
}

int rssiDbm() {
  return WiFi.status() == WL_CONNECTED ? WiFi.RSSI() : 0;
}

}  // namespace net
