// aggregator.h — accumulates 1 Hz samples into a 60 s window.
#pragma once

#include "sensors/sensors.h"

namespace agg {

// Result of one publish window.
struct Window {
  bool  haveBME = false;
  float tempC = NAN, humidityPct = NAN, pressureHpa = NAN;

  bool  haveUV = false;
  float uvIndex = NAN;

  bool  haveSoil = false;
  float soilPct = NAN;

  float windAvgKph = 0;
  float windGustKph = 0;
  float windDirDeg = 0;
  const char* windCardinal = "N";

  float rainMm = 0;        // accumulated during this window
  uint32_t samples = 0;
};

// Start a new window. rainBaselineMm = current monotonic rain total.
void reset(float rainBaselineMm);

// Fold one sample into the current window.
void addSample(const SensorReading &r);

// Close the window. currentTotalRainMm = latest monotonic rain total.
Window finalize(float currentTotalRainMm);

}  // namespace agg
