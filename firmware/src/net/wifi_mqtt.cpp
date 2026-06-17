// wifi_mqtt.cpp — WiFi + MQTT with auto-reconnect and a retained LWT status.
#include "net/wifi_mqtt.h"
#include "config.h"
#include "secrets.h"
#include "payload.h"

#include <WiFi.h>
#include <PubSubClient.h>

namespace {

WiFiClient   wifiClient;
PubSubClient mqtt(wifiClient);

char topicReadings[96];
char topicLightning[96];
char topicStatus[96];

uint32_t lastReconnectAttempt = 0;
const uint32_t RECONNECT_BACKOFF_MS = 5000;

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

  // LWT: retained "offline" status published by the broker if we drop.
  char willMsg[160];
  char ts[32];
  payload::isoNow(ts, sizeof ts);
  size_t wn = payload::buildStatus(willMsg, sizeof willMsg, /*online=*/false,
                                   WiFi.localIP().toString().c_str(), ts);
  (void)wn;

  bool ok = mqtt.connect(STATION_ID, MQTT_USER, MQTT_PASS,
                         topicStatus, MQTT_QOS, /*retain=*/true, willMsg);
  if (ok) {
    // Announce online (retained), so subscribers see current state on connect.
    char msg[160];
    payload::isoNow(ts, sizeof ts);
    size_t n = payload::buildStatus(msg, sizeof msg, /*online=*/true,
                                    WiFi.localIP().toString().c_str(), ts);
    mqtt.publish(topicStatus, (const uint8_t *)msg, n, /*retain=*/true);
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
