// sensors.cpp — drivers for the MicroMod Weather Carrier sensor suite.
//
// NOTE: This is a first-draft scaffold. The exact method names for the SparkFun
// BME280 / VEML6075 / AS3935 libraries should be confirmed against the versions
// PlatformIO resolves. The Weather Meter Kit API was verified against the
// upstream library. Integration points are isolated per sensor for easy fixes.
#include "sensors/sensors.h"
#include "config.h"

#include <Wire.h>
#include <SPI.h>
#include <SparkFun_Weather_Meter_Kit_Arduino_Library.h>

#if ENABLE_BME
#include <SparkFunBME280.h>
#endif
#if ENABLE_UV
#include <SparkFun_VEML6075_Arduino_Library.h>
#endif
#if ENABLE_LIGHTNING
#include <SparkFun_AS3935.h>
#endif

namespace {

SFEWeatherMeterKit weatherMeter(WIND_DIR_PIN, WIND_SPEED_PIN, RAIN_PIN);

#if ENABLE_BME
BME280 bme;
bool bmeOk = false;
#endif
#if ENABLE_UV
VEML6075 uv;
bool uvOk = false;
#endif
#if ENABLE_LIGHTNING
SparkFun_AS3935 lightning;
bool lightningOk = false;
volatile bool lightningFlag = false;
void IRAM_ATTR onLightningIRQ() { lightningFlag = true; }
#endif

float readSoilPct() {
  int raw = analogRead(SOIL_PIN);
  // Map raw -> 0..100% (higher raw = drier).
  float pct = 100.0f * (float)(SOIL_RAW_DRY - raw) /
              (float)(SOIL_RAW_DRY - SOIL_RAW_WET);
  if (pct < 0) pct = 0;
  if (pct > 100) pct = 100;
  return pct;
}

}  // namespace

namespace sensors {

void begin() {
  Wire.begin();

  analogReadResolution(12);            // ESP32 ADC: 12-bit (0..4095)

  // ---- Weather Meter Kit ----
  weatherMeter.setADCResolutionBits(12);
  SFEWeatherMeterKitCalibrationParams cal = weatherMeter.getCalibrationParams();
  cal.mmPerRainfallCount = MM_PER_RAIN_COUNT;
  cal.kphPerCountPerSec  = KPH_PER_COUNT_PER_S;
  // TODO: bench-calibrate cal.vaneADCValues[] for the ESP32 ADC.
  weatherMeter.setCalibrationParams(cal);
  weatherMeter.begin();

#if ENABLE_BME
  bme.setI2CAddress(BME280_ADDR);
  bmeOk = bme.beginI2C();
  if (!bmeOk) Serial.println("[sensors] BME280 init failed");
#endif

#if ENABLE_UV
  uvOk = uv.begin();
  if (!uvOk) Serial.println("[sensors] VEML6075 init failed");
#endif

#if ENABLE_LIGHTNING
  SPI.begin();
  lightningOk = lightning.beginSPI(LIGHTNING_CS_PIN);
  if (lightningOk) {
    lightning.setIndoorOutdoor(OUTDOOR);
    pinMode(LIGHTNING_INT_PIN, INPUT);
    attachInterrupt(digitalPinToInterrupt(LIGHTNING_INT_PIN), onLightningIRQ, RISING);
  } else {
    Serial.println("[sensors] AS3935 init failed");
  }
#endif
}

SensorReading read() {
  SensorReading r;

  r.windSpeedKph = weatherMeter.getWindSpeed();
  r.windDirDeg   = weatherMeter.getWindDirection();
  r.totalRainMm  = weatherMeter.getTotalRainfall();

#if ENABLE_BME
  if (bmeOk) {
    r.haveBME      = true;
    r.tempC        = bme.readTempC();
    r.humidityPct  = bme.readFloatHumidity();
    r.pressureHpa  = bme.readFloatPressure() / 100.0f;  // Pa -> hPa
  }
#endif

#if ENABLE_UV
  if (uvOk) {
    r.haveUV   = true;
    r.uvIndex  = uv.index();
  }
#endif

#if ENABLE_SOIL
  r.haveSoil = true;
  r.soilPct  = readSoilPct();
#endif

  return r;
}

bool pollLightning(LightningEvent &ev) {
#if ENABLE_LIGHTNING
  if (!lightningOk || !lightningFlag) return false;
  lightningFlag = false;

  // Reading the interrupt register also clears it on the AS3935.
  int intVal = lightning.readInterruptReg();
  if (intVal == LIGHTNING_INT) {
    ev.type       = "strike";
    ev.distanceKm = lightning.distanceToStorm();
    ev.energy     = lightning.lightningEnergy();
    return true;
  }
#if LIGHTNING_REPORT_NONSTRIKE
  if (intVal == DISTURBER_INT) { ev.type = "disturber"; ev.distanceKm = 0; ev.energy = 0; return true; }
  if (intVal == NOISE_INT)     { ev.type = "noise";     ev.distanceKm = 0; ev.energy = 0; return true; }
#endif
#endif
  return false;
}

}  // namespace sensors
