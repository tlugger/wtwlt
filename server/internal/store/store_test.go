package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/tlugger/wtwlt/server/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	// File-backed temp DB (modernc :memory: is per-connection; a temp file is simpler).
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func f(v float64) *float64 { return &v }

func TestInsertAndLatestReading(t *testing.T) {
	s := newTestStore(t)
	recv := time.Now().UTC()

	older := model.Reading{
		StationID: "wtwlt-01", IntervalS: 60,
		TempC: f(20.0),
		Wind:  model.Wind{AvgMPS: 1.0, DirCardinal: "N"},
		Diagnostics: model.Diagnostics{FWVersion: "1.0.0"},
	}
	newer := older
	newer.TempC = f(22.5)
	newer.SoilPct = nil // absent sensor -> NULL -> nil

	t1 := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 16, 12, 1, 0, 0, time.UTC)
	if err := s.InsertReading(older, t1, recv); err != nil {
		t.Fatalf("insert older: %v", err)
	}
	if err := s.InsertReading(newer, t2, recv); err != nil {
		t.Fatalf("insert newer: %v", err)
	}

	got, ts, err := s.LatestReading("wtwlt-01")
	if err != nil {
		t.Fatalf("LatestReading: %v", err)
	}
	if !ts.Equal(t2) {
		t.Errorf("latest ts = %v, want %v", ts, t2)
	}
	if got.TempC == nil || *got.TempC != 22.5 {
		t.Errorf("latest temp = %v, want 22.5", got.TempC)
	}
	if got.SoilPct != nil {
		t.Errorf("soil should be NULL->nil, got %v", got.SoilPct)
	}
}

func TestUpsertStatusAndStations(t *testing.T) {
	s := newTestStore(t)
	recv := time.Now().UTC()

	if err := s.UpsertStatus(model.Status{StationID: "wtwlt-01", Online: true, IP: "10.0.0.5"}, recv); err != nil {
		t.Fatalf("upsert online: %v", err)
	}
	// LWT flips it offline — upsert should overwrite, not duplicate.
	if err := s.UpsertStatus(model.Status{StationID: "wtwlt-01", Online: false, IP: "10.0.0.5"}, recv.Add(time.Minute)); err != nil {
		t.Fatalf("upsert offline: %v", err)
	}

	stations, err := s.Stations()
	if err != nil {
		t.Fatalf("Stations: %v", err)
	}
	if len(stations) != 1 {
		t.Fatalf("want 1 station, got %d", len(stations))
	}
	if stations[0].Online {
		t.Errorf("station should be offline after LWT")
	}
}

func TestInsertLightning(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()
	l := model.Lightning{StationID: "wtwlt-01", Event: "strike", DistanceKm: 12, Energy: 158473}
	if err := s.InsertLightning(l, now, now); err != nil {
		t.Fatalf("InsertLightning: %v", err)
	}
}
