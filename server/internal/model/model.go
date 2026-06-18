// Package model defines the MQTT message contract (SPEC.md §3.3) and parsing.
//
// Nullable sensor fields use *float64 so a JSON null (the firmware's NAN->null)
// round-trips as a nil pointer rather than 0.
package model

import (
	"encoding/json"
	"fmt"
	"time"
)

// Wind is the aggregated wind sub-object of a Reading (SI units).
type Wind struct {
	AvgMPS      float64 `json:"avg_mps"`
	GustMPS     float64 `json:"gust_mps"`
	DirDeg      float64 `json:"dir_deg"`
	DirCardinal string  `json:"dir_cardinal"`
}

// Diagnostics is the per-reading device health sub-object.
type Diagnostics struct {
	BatteryV  *float64 `json:"battery_v"`
	RSSIDbm   int      `json:"rssi_dbm"`
	UptimeS   int64    `json:"uptime_s"`
	FWVersion string   `json:"fw_version"`
}

// Reading is one aggregated 60 s message on .../readings.
type Reading struct {
	StationID   string      `json:"station_id"`
	TS          string      `json:"ts"`
	IntervalS   int         `json:"interval_s"`
	TempC       *float64    `json:"temp_c"`
	HumidityPct *float64    `json:"humidity_pct"`
	PressureHpa *float64    `json:"pressure_hpa"`
	UVIndex     *float64    `json:"uv_index"`
	Wind        Wind        `json:"wind"`
	RainMM      float64     `json:"rain_mm"`
	SoilPct     *float64    `json:"soil_moisture_pct"`
	Diagnostics Diagnostics `json:"diagnostics"`
}

// Lightning is one event message on .../lightning.
type Lightning struct {
	StationID  string `json:"station_id"`
	TS         string `json:"ts"`
	Event      string `json:"event"`
	DistanceKm int    `json:"distance_km"`
	Energy     int64  `json:"energy"`
}

// Status is the retained/LWT message on .../status.
type Status struct {
	StationID string `json:"station_id"`
	Online    bool   `json:"online"`
	FWVersion string `json:"fw_version"`
	IP        string `json:"ip"`
	BootTS    string `json:"boot_ts"`
}

// ParseReading decodes and validates a readings payload.
func ParseReading(b []byte) (Reading, error) {
	var r Reading
	if err := json.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("readings: %w", err)
	}
	if r.StationID == "" {
		return r, fmt.Errorf("readings: missing station_id")
	}
	return r, nil
}

// ParseLightning decodes and validates a lightning payload.
func ParseLightning(b []byte) (Lightning, error) {
	var l Lightning
	if err := json.Unmarshal(b, &l); err != nil {
		return l, fmt.Errorf("lightning: %w", err)
	}
	if l.StationID == "" {
		return l, fmt.Errorf("lightning: missing station_id")
	}
	return l, nil
}

// ParseStatus decodes and validates a status payload.
func ParseStatus(b []byte) (Status, error) {
	var s Status
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("status: %w", err)
	}
	if s.StationID == "" {
		return s, fmt.Errorf("status: missing station_id")
	}
	return s, nil
}

// EventTime returns the message timestamp parsed as UTC. If the firmware's `ts`
// is missing or unparseable, it falls back to the supplied receive time (the
// SPEC §3.3 ingest fallback) and reports ok=false.
func EventTime(ts string, received time.Time) (t time.Time, ok bool) {
	if ts == "" {
		return received.UTC(), false
	}
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return received.UTC(), false
	}
	return parsed.UTC(), true
}
