// sensors.h — sensor bring-up and reads for the MicroMod Weather Carrier.
#pragma once

// Note: this header intentionally avoids <Arduino.h> so the data structs below
// (and the aggregator that consumes them) compile on the host for native unit
// tests. .cpp files that need Arduino include config.h / <Arduino.h> themselves.
#include <stdint.h>
#include <math.h>

// One instantaneous sample. NAN / have-flags mark sensors that are absent or
// failed to initialize so the payload can emit null instead of garbage.
struct SensorReading {
  bool  haveBME = false;
  float tempC = NAN;
  float humidityPct = NAN;
  float pressureHpa = NAN;

  bool  haveUV = false;
  float uvIndex = NAN;

  bool  haveSoil = false;
  float soilPct = NAN;

  // Weather Meter Kit (always present)
  float windSpeedKph = NAN;   // instantaneous, this sample
  float windDirDeg   = NAN;   // instantaneous, this sample
  float totalRainMm  = 0.0f;  // monotonic accumulator (delta computed downstream)
};

struct LightningEvent {
  const char* type;       // "strike" | "disturber" | "noise"
  uint8_t     distanceKm; // 0 = overhead/unknown for disturber/noise
  uint32_t    energy;     // raw AS3935 energy units (relative only)
};

namespace sensors {
  void begin();                          // init every enabled sensor
  SensorReading read();                  // fast read for the 1 Hz tick
  bool pollLightning(LightningEvent &ev); // true if an event is pending
  void printRaw();                       // bench calibration: dump raw values to serial
}
