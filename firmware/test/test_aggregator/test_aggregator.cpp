// Host-based unit tests for the 60 s aggregation logic (agg::).
// Run with:  pio test -e native
#include <unity.h>
#include <math.h>
#include "aggregator.h"

// Helper: build a sample with the fields the aggregator reads.
static SensorReading sample(float windKph, float dirDeg, float rainTotal) {
  SensorReading r;
  r.windSpeedKph = windKph;
  r.windDirDeg   = dirDeg;
  r.totalRainMm  = rainTotal;
  return r;
}

void setUp() {}
void tearDown() {}

// --- Wind speed: average over valid samples, plus peak gust ---
void test_wind_average_and_gust() {
  agg::reset(0);
  agg::addSample(sample(2.0f, 0, 0));
  agg::addSample(sample(4.0f, 0, 0));
  agg::addSample(sample(6.0f, 0, 0));
  agg::Window w = agg::finalize(0);

  TEST_ASSERT_EQUAL_UINT32(3, w.samples);
  TEST_ASSERT_FLOAT_WITHIN(0.001f, 4.0f, w.windAvgKph);  // (2+4+6)/3
  TEST_ASSERT_FLOAT_WITHIN(0.001f, 6.0f, w.windGustKph);
}

// NAN wind samples must not dilute the average (regression: previously divided
// by total sample count instead of valid-wind count).
void test_nan_wind_excluded_from_average() {
  agg::reset(0);
  agg::addSample(sample(NAN, NAN, 0));
  agg::addSample(sample(4.0f, 90.0f, 0));
  agg::Window w = agg::finalize(0);

  TEST_ASSERT_EQUAL_UINT32(2, w.samples);
  TEST_ASSERT_FLOAT_WITHIN(0.001f, 4.0f, w.windAvgKph);  // only the one valid sample
}

// --- Wind direction: vector average must handle the 0/360 wraparound ---
void test_wind_direction_wraps_around_north() {
  agg::reset(0);
  agg::addSample(sample(1, 350.0f, 0));
  agg::addSample(sample(1, 10.0f, 0));
  agg::Window w = agg::finalize(0);

  // Mean of 350 and 10 is 0 (= 360), NOT the naive scalar mean of 180.
  bool nearNorth = (w.windDirDeg < 1.0f) || (w.windDirDeg > 359.0f);
  TEST_ASSERT_TRUE(nearNorth);
  TEST_ASSERT_EQUAL_STRING("N", w.windCardinal);
}

void test_cardinal_labels() {
  agg::reset(0);
  agg::addSample(sample(1, 90.0f, 0));
  TEST_ASSERT_EQUAL_STRING("E", agg::finalize(0).windCardinal);

  agg::reset(0);
  agg::addSample(sample(1, 180.0f, 0));
  TEST_ASSERT_EQUAL_STRING("S", agg::finalize(0).windCardinal);

  agg::reset(0);
  agg::addSample(sample(1, 270.0f, 0));
  TEST_ASSERT_EQUAL_STRING("W", agg::finalize(0).windCardinal);
}

// --- Rain: delta of the monotonic accumulator over the window ---
void test_rain_delta() {
  agg::reset(5.0f);  // baseline 5 mm at window start
  agg::Window w = agg::finalize(5.2794f);
  TEST_ASSERT_FLOAT_WITHIN(0.0001f, 0.2794f, w.rainMm);  // one bucket tip
}

// A backwards-moving counter (device/library reset) must not yield negative rain.
void test_rain_counter_reset_guard() {
  agg::reset(10.0f);
  agg::Window w = agg::finalize(2.0f);
  TEST_ASSERT_EQUAL_FLOAT(0.0f, w.rainMm);
}

// --- Scalar sensors carry through their have-flags ---
void test_scalar_passthrough_and_flags() {
  agg::reset(0);
  SensorReading r = sample(1, 0, 0);
  r.haveBME = true;  r.tempC = 21.5f;  r.humidityPct = 60.0f;  r.pressureHpa = 1012.0f;
  r.haveUV  = false; r.uvIndex = NAN;
  agg::addSample(r);
  agg::Window w = agg::finalize(0);

  TEST_ASSERT_TRUE(w.haveBME);
  TEST_ASSERT_FALSE(w.haveUV);
  TEST_ASSERT_FLOAT_WITHIN(0.001f, 21.5f, w.tempC);
  TEST_ASSERT_FLOAT_WITHIN(0.001f, 1012.0f, w.pressureHpa);
}

int runUnityTests() {
  UNITY_BEGIN();
  RUN_TEST(test_wind_average_and_gust);
  RUN_TEST(test_nan_wind_excluded_from_average);
  RUN_TEST(test_wind_direction_wraps_around_north);
  RUN_TEST(test_cardinal_labels);
  RUN_TEST(test_rain_delta);
  RUN_TEST(test_rain_counter_reset_guard);
  RUN_TEST(test_scalar_passthrough_and_flags);
  return UNITY_END();
}

#if defined(ARDUINO)
#include <Arduino.h>
void setup() { runUnityTests(); }
void loop() {}
#else
int main() { return runUnityTests(); }
#endif
