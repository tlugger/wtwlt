// payload.h — JSON serialization of the MQTT contract (see SPEC.md §3.3).
#pragma once

#include <Arduino.h>
#include "aggregator.h"
#include "sensors/sensors.h"

struct Diagnostics {
  float    batteryV;   // NAN -> emitted as null
  int      rssiDbm;
  uint32_t uptimeS;
};

namespace payload {

// Fill `buf` with UTC ISO-8601 ("2026-06-16T12:00:00Z"). Falls back to a
// boot-relative marker if SNTP time isn't available yet.
void isoNow(char *buf, size_t len);

size_t buildReadings(char *buf, size_t len, const agg::Window &w,
                     const Diagnostics &diag, const char *tsIso);
size_t buildLightning(char *buf, size_t len, const LightningEvent &ev,
                      const char *tsIso);
size_t buildStatus(char *buf, size_t len, bool online, const char *ip,
                   const char *tsIso);

}  // namespace payload
