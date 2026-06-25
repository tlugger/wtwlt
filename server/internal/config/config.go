// Package config loads server configuration from the environment.
package config

import (
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	MQTTHost      string
	MQTTPort      string
	MQTTUser      string
	MQTTPass      string
	HTTPAddr      string
	DBPath        string
	RetentionDays int // raw readings older than this are pruned (rollups kept); 0 = keep all

	// Forecast overlay: a keyless provider polled on a timer, stored separately
	// from sensor data and drawn as a dashed projection on the dashboard.
	ForecastProvider string  // "openmeteo" | "nws" | "none"
	ForecastMinutes  int     // poll interval
	Lat              float64 // station latitude  (forecast location)
	Lon              float64 // station longitude (forecast location)
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getenvFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

// Load reads WTWLT_* environment variables, applying sensible local defaults.
func Load() Config {
	dataDir := getenv("WTWLT_DATA_DIR", ".")
	return Config{
		MQTTHost:      getenv("WTWLT_MQTT_HOST", "localhost"),
		MQTTPort:      getenv("WTWLT_MQTT_PORT", "1883"),
		MQTTUser:      os.Getenv("WTWLT_MQTT_USER"),
		MQTTPass:      os.Getenv("WTWLT_MQTT_PASS"),
		HTTPAddr:      getenv("WTWLT_HTTP_ADDR", ":8080"),
		DBPath:        getenv("WTWLT_DB_PATH", filepath.Join(dataDir, "wtwlt.db")),
		RetentionDays: getenvInt("WTWLT_RETENTION_DAYS", 90),

		ForecastProvider: getenv("WTWLT_FORECAST_PROVIDER", "openmeteo"),
		ForecastMinutes:  getenvInt("WTWLT_FORECAST_MINUTES", 60),
		Lat:              getenvFloat("WTWLT_LAT", 39.7392), // Denver, CO
		Lon:              getenvFloat("WTWLT_LON", -104.9903),
	}
}
