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

// Power the soil sensor only while reading (avoids continuous-power corrosion).
int readSoilRaw() {
  digitalWrite(SOIL_POWER_PIN, HIGH);
  delay(10);  // settle
  int raw = analogRead(SOIL_PIN);
  digitalWrite(SOIL_POWER_PIN, LOW);
  return raw;
}

// Linear map raw -> 0..100% between the calibrated dry/wet endpoints.
float soilPctFromRaw(int raw) {
  float pct = 100.0f * (float)(SOIL_RAW_DRY - raw) /
              (float)(SOIL_RAW_DRY - SOIL_RAW_WET);
  if (pct <= 0) pct = 0;        // <= also normalizes -0.0 to 0
  if (pct > 100) pct = 100;
  return pct;
}

float readSoilPct() { return soilPctFromRaw(readSoilRaw()); }

// Vane bearing with the mount-day North-alignment offset applied (mod 360).
float windDirection() {
  float d = weatherMeter.getWindDirection() + WIND_DIR_OFFSET_DEG;
  d = fmodf(d, 360.0f);
  if (d < 0) d += 360.0f;
  return d;
}

}  // namespace

namespace {
// Bench diagnostic: log every device that ACKs on the I2C bus. Helps confirm
// which sensors are actually present (e.g. BME280 0x77, VEML6075 0x10).
void i2cScan() {
  Serial.print("[sensors] I2C scan:");
  uint8_t found = 0;
  for (uint8_t addr = 1; addr < 127; addr++) {
    Wire.beginTransmission(addr);
    if (Wire.endTransmission() == 0) {
      Serial.printf(" 0x%02X", addr);
      found++;
    }
  }
  Serial.println(found ? "" : " (no devices found)");
}
}  // namespace

namespace sensors {

void begin() {
  Wire.begin();

#if DEBUG_I2C_SCAN
  i2cScan();
#endif

  analogReadResolution(12);            // ESP32 ADC: 12-bit (0..4095)

#if ENABLE_SOIL
  pinMode(SOIL_POWER_PIN, OUTPUT);
  digitalWrite(SOIL_POWER_PIN, LOW);   // power-gated; only HIGH during a read
#endif

  // ---- Weather Meter Kit ----
  weatherMeter.setADCResolutionBits(12);
  SFEWeatherMeterKitCalibrationParams cal = weatherMeter.getCalibrationParams();
  cal.mmPerRainfallCount = MM_PER_RAIN_COUNT;
  cal.kphPerCountPerSec  = KPH_PER_COUNT_PER_S;
  for (int i = 0; i < 16; i++) cal.vaneADCValues[i] = VANE_ADC_VALUES[i];
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
  r.windDirDeg   = windDirection();
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

void printRaw() {
  int soil = readSoilRaw();
  Serial.printf("[cal] vaneADC=%4d dir=%5.1f | soil=%5.1f%% | wsCnt=%lu rainCnt=%lu | wsPin=%d rainPin=%d\n",
                analogRead(WIND_DIR_PIN), windDirection(),
                soilPctFromRaw(soil),
                (unsigned long)weatherMeter.getWindSpeedCounts(),
                (unsigned long)weatherMeter.getRainfallCounts(),
                digitalRead(WIND_SPEED_PIN), digitalRead(RAIN_PIN));
}

bool pollLightning(LightningEvent &ev) {
#if ENABLE_LIGHTNING
  if (!lightningOk || !lightningFlag) return false;
  lightningFlag = false;

  // Reading the interrupt register also clears it on the AS3935.
  int intVal = lightning.readInterruptReg();
  if (intVal == LIGHTNING) {
    ev.type       = "strike";
    ev.distanceKm = lightning.distanceToStorm();
    ev.energy     = lightning.lightningEnergy();
    return true;
  }
#if LIGHTNING_REPORT_NONSTRIKE
  if (intVal == DISTURBER_DETECT) { ev.type = "disturber"; ev.distanceKm = 0; ev.energy = 0; return true; }
  if (intVal == NOISE_TO_HIGH)    { ev.type = "noise";     ev.distanceKm = 0; ev.energy = 0; return true; }
#endif
#endif
  return false;
}

}  // namespace sensors
