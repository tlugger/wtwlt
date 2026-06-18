// Package config loads server configuration from the environment.
package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	MQTTHost string
	MQTTPort string
	MQTTUser string
	MQTTPass string
	HTTPAddr string
	DBPath   string
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Load reads WTWLT_* environment variables, applying sensible local defaults.
func Load() Config {
	dataDir := getenv("WTWLT_DATA_DIR", ".")
	return Config{
		MQTTHost: getenv("WTWLT_MQTT_HOST", "localhost"),
		MQTTPort: getenv("WTWLT_MQTT_PORT", "1883"),
		MQTTUser: os.Getenv("WTWLT_MQTT_USER"),
		MQTTPass: os.Getenv("WTWLT_MQTT_PASS"),
		HTTPAddr: getenv("WTWLT_HTTP_ADDR", ":8080"),
		DBPath:   getenv("WTWLT_DB_PATH", filepath.Join(dataDir, "wtwlt.db")),
	}
}
