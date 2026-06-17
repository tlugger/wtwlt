// aggregator.cpp — windowed aggregation of sensor samples.
#include "aggregator.h"
#include <math.h>

namespace {

uint32_t g_samples = 0;
float    g_rainBaseline = 0;

// Wind speed
float    g_windSum = 0;
float    g_windGust = 0;
uint32_t g_windCount = 0;   // count of valid (non-NAN) wind samples

// Wind direction as unit vectors (avoids the 0/360 wraparound problem)
double g_dirSin = 0;
double g_dirCos = 0;

// Latest scalar readings (instantaneous at publish time)
SensorReading g_last;

const char* cardinal(float deg) {
  static const char* dirs[] = {"N","NNE","NE","ENE","E","ESE","SE","SSE",
                               "S","SSW","SW","WSW","W","WNW","NW","NNW"};
  int idx = (int)((deg + 11.25f) / 22.5f) & 15;
  return dirs[idx];
}

}  // namespace

namespace agg {

void reset(float rainBaselineMm) {
  g_samples = 0;
  g_rainBaseline = rainBaselineMm;
  g_windSum = 0;
  g_windGust = 0;
  g_windCount = 0;
  g_dirSin = 0;
  g_dirCos = 0;
}

void addSample(const SensorReading &r) {
  g_samples++;
  g_last = r;

  if (!isnan(r.windSpeedKph)) {
    g_windSum += r.windSpeedKph;
    g_windCount++;
    if (r.windSpeedKph > g_windGust) g_windGust = r.windSpeedKph;
  }
  if (!isnan(r.windDirDeg)) {
    double rad = r.windDirDeg * M_PI / 180.0;
    g_dirSin += sin(rad);
    g_dirCos += cos(rad);
  }
}

Window finalize(float currentTotalRainMm) {
  Window w;
  w.samples = g_samples;

  w.windAvgKph  = g_windCount ? (g_windSum / g_windCount) : 0;
  w.windGustKph = g_windGust;

  float dir = atan2(g_dirSin, g_dirCos) * 180.0 / M_PI;
  if (dir < 0) dir += 360.0f;
  w.windDirDeg   = dir;
  w.windCardinal = cardinal(dir);

  float rain = currentTotalRainMm - g_rainBaseline;
  w.rainMm = rain > 0 ? rain : 0;  // guard against counter resets

  // Scalar sensors: report the most recent sample's values.
  w.haveBME      = g_last.haveBME;
  w.tempC        = g_last.tempC;
  w.humidityPct  = g_last.humidityPct;
  w.pressureHpa  = g_last.pressureHpa;
  w.haveUV       = g_last.haveUV;
  w.uvIndex      = g_last.uvIndex;
  w.haveSoil     = g_last.haveSoil;
  w.soilPct      = g_last.soilPct;

  return w;
}

}  // namespace agg
