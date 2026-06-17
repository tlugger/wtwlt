// wifi_mqtt.h — WiFi + MQTT lifecycle (connect, reconnect, publish).
#pragma once

#include <Arduino.h>

namespace net {

void begin();   // connect WiFi, start SNTP, configure MQTT + LWT
void loop();    // maintain connections; must be called frequently
bool connected();

void publishReadings(const char *json, size_t n);
void publishLightning(const char *json, size_t n);

int rssiDbm();  // current WiFi RSSI

}  // namespace net
