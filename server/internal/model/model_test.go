package model

import (
	"testing"
	"time"
)

func TestParseReadingFull(t *testing.T) {
	payload := []byte(`{
	  "station_id":"wtwlt-01","ts":"2026-06-16T12:00:00Z","interval_s":60,
	  "temp_c":21.4,"humidity_pct":58.2,"pressure_hpa":1013.2,"uv_index":3.1,
	  "wind":{"avg_mps":2.4,"gust_mps":5.1,"dir_deg":270,"dir_cardinal":"W"},
	  "rain_mm":0.5,"soil_moisture_pct":42.0,
	  "diagnostics":{"battery_v":3.92,"rssi_dbm":-67,"uptime_s":38211,"fw_version":"1.0.0"}
	}`)

	r, err := ParseReading(payload)
	if err != nil {
		t.Fatalf("ParseReading: %v", err)
	}
	if r.StationID != "wtwlt-01" {
		t.Errorf("station_id = %q", r.StationID)
	}
	if r.TempC == nil || *r.TempC != 21.4 {
		t.Errorf("temp_c = %v", r.TempC)
	}
	if r.Wind.AvgMPS != 2.4 || r.Wind.DirCardinal != "W" {
		t.Errorf("wind = %+v", r.Wind)
	}
	if r.Diagnostics.BatteryV == nil || *r.Diagnostics.BatteryV != 3.92 {
		t.Errorf("battery_v = %v", r.Diagnostics.BatteryV)
	}
	if r.Diagnostics.FWVersion != "1.0.0" {
		t.Errorf("fw_version = %q", r.Diagnostics.FWVersion)
	}
}

func TestParseReadingNulls(t *testing.T) {
	// Absent sensors / battery arrive as JSON null -> nil pointers.
	payload := []byte(`{
	  "station_id":"wtwlt-01","ts":"2026-06-16T12:00:00Z","interval_s":60,
	  "temp_c":null,"humidity_pct":null,"pressure_hpa":null,"uv_index":null,
	  "wind":{"avg_mps":0,"gust_mps":0,"dir_deg":0,"dir_cardinal":"N"},
	  "rain_mm":0,"soil_moisture_pct":null,
	  "diagnostics":{"battery_v":null,"rssi_dbm":0,"uptime_s":0,"fw_version":"1.0.0"}
	}`)

	r, err := ParseReading(payload)
	if err != nil {
		t.Fatalf("ParseReading: %v", err)
	}
	if r.TempC != nil || r.SoilPct != nil || r.Diagnostics.BatteryV != nil {
		t.Errorf("expected nil pointers for null fields, got temp=%v soil=%v batt=%v",
			r.TempC, r.SoilPct, r.Diagnostics.BatteryV)
	}
}

func TestParseReadingMissingStation(t *testing.T) {
	if _, err := ParseReading([]byte(`{"ts":"x"}`)); err == nil {
		t.Fatal("expected error for missing station_id")
	}
}

func TestParseLightningAndStatus(t *testing.T) {
	l, err := ParseLightning([]byte(`{"station_id":"wtwlt-01","ts":"2026-06-16T12:00:03Z","event":"strike","distance_km":12,"energy":158473}`))
	if err != nil {
		t.Fatalf("ParseLightning: %v", err)
	}
	if l.Event != "strike" || l.DistanceKm != 12 || l.Energy != 158473 {
		t.Errorf("lightning = %+v", l)
	}

	s, err := ParseStatus([]byte(`{"station_id":"wtwlt-01","online":true,"fw_version":"1.0.0","ip":"192.168.1.42","boot_ts":"2026-06-16T01:23:45Z"}`))
	if err != nil {
		t.Fatalf("ParseStatus: %v", err)
	}
	if !s.Online || s.IP != "192.168.1.42" {
		t.Errorf("status = %+v", s)
	}
}

func TestEventTime(t *testing.T) {
	recv := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)

	got, ok := EventTime("2026-06-16T12:00:00Z", recv)
	if !ok || !got.Equal(time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("valid ts: got %v ok=%v", got, ok)
	}

	got, ok = EventTime("", recv)
	if ok || !got.Equal(recv) {
		t.Errorf("empty ts should fall back to received: got %v ok=%v", got, ok)
	}

	if _, ok := EventTime("not-a-time", recv); ok {
		t.Error("invalid ts should report ok=false")
	}
}
