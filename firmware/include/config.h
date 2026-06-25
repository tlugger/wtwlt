// config.h — checked-in tunables, pin map, and calibration.
// Credentials live in secrets.h (see secrets.example.h).
#pragma once

// <stdint.h> (not <Arduino.h>) so this header — and payload.cpp, which includes
// it — compiles on the host for native unit tests. Only fixed-width int types
// are needed here.
#include <stdint.h>

// ---------------------------------------------------------------------------
// Firmware identity
// ---------------------------------------------------------------------------
#define FW_VERSION "1.0.0"

// ---------------------------------------------------------------------------
// Cadence
// ---------------------------------------------------------------------------
static const uint32_t SAMPLE_INTERVAL_MS  = 1000;   // sensor sample tick (1 Hz)
static const uint32_t PUBLISH_INTERVAL_MS = 60000;  // aggregated publish (60 s)

// ---------------------------------------------------------------------------
// MQTT topics:  <TOPIC_PREFIX>/<STATION_ID>/{readings,lightning,status}
// ---------------------------------------------------------------------------
#define TOPIC_PREFIX "wtwlt/station"
static const uint8_t  MQTT_QOS         = 1;
static const uint16_t MQTT_BUFFER_SIZE = 512;  // payload is larger than PubSubClient's 256 default

// ---------------------------------------------------------------------------
// Time sync (SNTP) — timestamps are emitted as UTC ISO-8601
// ---------------------------------------------------------------------------
#define NTP_SERVER "pool.ntp.org"

// ---------------------------------------------------------------------------
// Pin map — SparkFun MicroMod Weather Carrier + ESP32 MicroMod Processor.
// Source: SparkFun MicroMod Weather Carrier hookup guide.
// ---------------------------------------------------------------------------
static const uint8_t WIND_DIR_PIN   = 35;  // A1, ADC1 (analog vane) — ADC1 is WiFi-safe
static const uint8_t WIND_SPEED_PIN = 23;  // D0  (anemometer reed switch)
static const uint8_t RAIN_PIN       = 27;  // D1  (rain gauge reed switch)
static const uint8_t SOIL_PIN       = 39;  // A0, ADC1 (analog soil moisture terminal)

// AS3935 lightning detector — SPI, chip select on G1/BUS1.
static const uint8_t LIGHTNING_CS_PIN  = 12;
// VERIFY: AS3935 interrupt routing is not confirmed in the hookup guide.
// Confirm against your board before trusting lightning events.
static const uint8_t LIGHTNING_INT_PIN = 4;

// I2C addresses (onboard)
static const uint8_t BME280_ADDR   = 0x77;
static const uint8_t VEML6075_ADDR = 0x10;

// ---------------------------------------------------------------------------
// Feature toggles (disable a sensor that isn't populated / wired)
// ---------------------------------------------------------------------------
#define ENABLE_BME       1
#define ENABLE_UV        1
#define ENABLE_LIGHTNING 1
#define ENABLE_SOIL      1
// Log all responding I2C addresses at boot (bench bring-up diagnostic).
#define DEBUG_I2C_SCAN   1
// Publish disturber/noise lightning events too (else only real strikes)?
#define LIGHTNING_REPORT_NONSTRIKE 0

// ---------------------------------------------------------------------------
// Weather Meter Kit calibration (SFEWeatherMeterKit defaults shown).
// vaneADCValues[] must be re-calibrated for the ESP32's 12-bit nonlinear ADC.
// ---------------------------------------------------------------------------
static const float MM_PER_RAIN_COUNT   = 0.2794f;  // mm per bucket tip
static const float KPH_PER_COUNT_PER_S = 2.4f;     // wind speed per count/sec

// ---------------------------------------------------------------------------
// Soil moisture raw(ADC)->% calibration. Bench-calibrate dry/wet endpoints.
// 12-bit ADC: 0..4095. Typical: higher raw = drier.
// ---------------------------------------------------------------------------
static const int SOIL_RAW_DRY = 3200;  // reading in dry air
static const int SOIL_RAW_WET = 1300;  // reading fully submerged

// ---------------------------------------------------------------------------
// Battery monitoring (optional). Set BATTERY_ADC_PIN to a valid pin to enable;
// leave as 0xFF to report battery_v as null.
// ---------------------------------------------------------------------------
static const uint8_t BATTERY_ADC_PIN = 0xFF;
static const float   BATTERY_DIVIDER = 2.0f;  // (R1+R2)/R2 of the sense divider
static const float   ADC_REF_VOLTS   = 3.3f;
static const int     ADC_MAX_COUNTS  = 4095;
