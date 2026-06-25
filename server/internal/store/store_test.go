package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/tlugger/wtwlt/server/internal/forecast"
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
		TempC:       f(20.0),
		Wind:        model.Wind{AvgMPS: 1.0, DirCardinal: "N"},
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

// seed inserts a reading at ts with the given temp/gust/rain (other sensors nil).
func seedReading(t *testing.T, s *Store, station string, ts time.Time, temp, gust, rain float64) {
	t.Helper()
	r := model.Reading{
		StationID:   station,
		Wind:        model.Wind{GustMPS: gust, DirCardinal: "N"},
		RainMM:      rain,
		TempC:       &temp,
		Diagnostics: model.Diagnostics{FWVersion: "1.0.0"},
	}
	if err := s.InsertReading(r, ts, ts); err != nil {
		t.Fatalf("seed reading: %v", err)
	}
}

func TestHistoryBucketing(t *testing.T) {
	s := newTestStore(t)
	base := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	// Two readings in hour 10, one in hour 11.
	seedReading(t, s, "wtwlt-01", base, 20.0, 5.0, 0.2)
	seedReading(t, s, "wtwlt-01", base.Add(30*time.Minute), 22.0, 9.0, 0.3)
	seedReading(t, s, "wtwlt-01", base.Add(90*time.Minute), 24.0, 4.0, 0.1)

	from := base.Add(-time.Hour)
	to := base.Add(3 * time.Hour)

	if err := s.RollupHourly(from); err != nil {
		t.Fatal(err)
	}
	if err := s.RollupDaily(from); err != nil {
		t.Fatal(err)
	}

	hourly, err := s.History("wtwlt-01", from, to, "hour")
	if err != nil {
		t.Fatalf("History hour: %v", err)
	}
	if len(hourly) != 2 {
		t.Fatalf("want 2 hourly buckets, got %d", len(hourly))
	}
	// Hour 10: avg temp = 21, max gust = 9, rain sum = 0.5, count 2.
	h10 := hourly[0]
	if h10.Count != 2 {
		t.Errorf("hour10 count = %d", h10.Count)
	}
	if h10.TempC == nil || *h10.TempC != 21.0 {
		t.Errorf("hour10 avg temp = %v, want 21", h10.TempC)
	}
	if h10.WindGustMPS == nil || *h10.WindGustMPS != 9.0 {
		t.Errorf("hour10 max gust = %v, want 9", h10.WindGustMPS)
	}
	if diff := h10.RainMM - 0.5; diff < -1e-9 || diff > 1e-9 {
		t.Errorf("hour10 rain sum = %v, want 0.5", h10.RainMM)
	}
	if !h10.Bucket.Equal(time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("hour10 bucket = %v", h10.Bucket)
	}

	// Day bucket: all three collapse into one day.
	daily, err := s.History("wtwlt-01", from, to, "day")
	if err != nil {
		t.Fatalf("History day: %v", err)
	}
	if len(daily) != 1 || daily[0].Count != 3 {
		t.Fatalf("want 1 daily bucket of 3, got %+v", daily)
	}
}

func TestHistoryInvalidBucket(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.History("wtwlt-01", time.Time{}, time.Now(), "week"); err == nil {
		t.Fatal("expected error for invalid bucket")
	}
}

func TestRollupSurvivesPrune(t *testing.T) {
	s := newTestStore(t)
	old := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	seedReading(t, s, "wtwlt-01", old, 10.0, 3.0, 0.1)
	seedReading(t, s, "wtwlt-01", old.Add(20*time.Minute), 14.0, 7.0, 0.2)

	if err := s.RollupHourly(old.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.RollupDaily(old.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	n, err := s.PruneRaw(old.Add(24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("pruned %d, want 2", n)
	}

	// raw is gone...
	if raw, _ := s.History("wtwlt-01", old.Add(-time.Hour), old.Add(2*time.Hour), "raw"); len(raw) != 0 {
		t.Errorf("expected raw pruned, got %d rows", len(raw))
	}
	// ...but the hourly rollup remains with correct aggregates
	hourly, err := s.History("wtwlt-01", old.Add(-time.Hour), old.Add(2*time.Hour), "hour")
	if err != nil {
		t.Fatal(err)
	}
	if len(hourly) != 1 || hourly[0].Count != 2 {
		t.Fatalf("hourly = %+v", hourly)
	}
	if hourly[0].TempC == nil || *hourly[0].TempC != 12.0 {
		t.Errorf("avg temp = %v, want 12", hourly[0].TempC)
	}
	if hourly[0].WindGustMPS == nil || *hourly[0].WindGustMPS != 7.0 {
		t.Errorf("max gust = %v, want 7", hourly[0].WindGustMPS)
	}
	if diff := hourly[0].RainMM - 0.3; diff < -1e-9 || diff > 1e-9 {
		t.Errorf("rain sum = %v, want 0.3", hourly[0].RainMM)
	}
}

func TestRollupIdempotent(t *testing.T) {
	s := newTestStore(t)
	base := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	seedReading(t, s, "wtwlt-01", base, 20.0, 5.0, 0.2)
	for i := 0; i < 3; i++ {
		if err := s.RollupHourly(base.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	hourly, _ := s.History("wtwlt-01", base.Add(-time.Hour), base.Add(time.Hour), "hour")
	if len(hourly) != 1 {
		t.Fatalf("idempotent rollup produced %d rows, want 1", len(hourly))
	}
}

func TestSummarize(t *testing.T) {
	s := newTestStore(t)
	base := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	seedReading(t, s, "wtwlt-01", base, 18.0, 5.0, 0.2)
	seedReading(t, s, "wtwlt-01", base.Add(time.Hour), 26.0, 12.0, 0.3)

	sum, err := s.Summarize("wtwlt-01", base.Add(-time.Hour), base.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if sum.Count != 2 {
		t.Errorf("count = %d", sum.Count)
	}
	if sum.TempMinC == nil || *sum.TempMinC != 18.0 {
		t.Errorf("temp min = %v", sum.TempMinC)
	}
	if sum.TempMaxC == nil || *sum.TempMaxC != 26.0 {
		t.Errorf("temp max = %v", sum.TempMaxC)
	}
	if sum.TempAvgC == nil || *sum.TempAvgC != 22.0 {
		t.Errorf("temp avg = %v", sum.TempAvgC)
	}
	if sum.WindGustMaxMPS == nil || *sum.WindGustMaxMPS != 12.0 {
		t.Errorf("gust max = %v", sum.WindGustMaxMPS)
	}
	if diff := sum.RainTotalMM - 0.5; diff < -1e-9 || diff > 1e-9 {
		t.Errorf("rain total = %v, want 0.5", sum.RainTotalMM)
	}
}

func TestSummarizeEmpty(t *testing.T) {
	s := newTestStore(t)
	sum, err := s.Summarize("nobody", time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatalf("Summarize empty: %v", err)
	}
	if sum.Count != 0 || sum.TempAvgC != nil || sum.RainTotalMM != 0 {
		t.Errorf("empty summary should be zero-valued, got %+v", sum)
	}
}

func TestLightningEvents(t *testing.T) {
	s := newTestStore(t)
	base := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	for i, km := range []int{5, 10, 15} {
		l := model.Lightning{StationID: "wtwlt-01", Event: "strike", DistanceKm: km, Energy: int64(1000 * (i + 1))}
		ts := base.Add(time.Duration(i) * time.Minute)
		if err := s.InsertLightning(l, ts, ts); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	events, err := s.LightningEvents("wtwlt-01", base.Add(-time.Hour), base.Add(time.Hour), 100)
	if err != nil {
		t.Fatalf("LightningEvents: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("want 3 events, got %d", len(events))
	}
	// Newest first.
	if events[0].DistanceKm != 15 {
		t.Errorf("first event should be newest (15km), got %d", events[0].DistanceKm)
	}

	// Limit is respected.
	limited, err := s.LightningEvents("wtwlt-01", base.Add(-time.Hour), base.Add(time.Hour), 2)
	if err != nil {
		t.Fatalf("LightningEvents limit: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("want 2 with limit, got %d", len(limited))
	}
}

func TestForecastUpsertAndRead(t *testing.T) {
	s := newTestStore(t)
	base := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	fetched := base.Add(-time.Minute)

	pts := []forecast.Point{
		{TS: base, TempC: f(18), HumidityPct: f(55), PressureHpa: f(840), PrecipMm: f(0), PrecipProb: f(20), CloudPct: f(40), WindMps: f(3), WindDirDeg: f(270), Condition: forecast.CondClear},
		{TS: base.Add(time.Hour), TempC: f(20), WindMps: f(4)}, // pressure/precip/prob/cloud absent -> nil
	}
	if err := s.UpsertForecast("openmeteo", pts, fetched); err != nil {
		t.Fatalf("UpsertForecast: %v", err)
	}

	// Empty source auto-selects the only stored source.
	got, src, err := s.Forecast("", base, base.Add(48*time.Hour))
	if err != nil {
		t.Fatalf("Forecast: %v", err)
	}
	if src != "openmeteo" {
		t.Errorf("source = %q, want openmeteo", src)
	}
	if len(got) != 2 {
		t.Fatalf("got %d points, want 2", len(got))
	}
	if got[0].TempC == nil || *got[0].TempC != 18 {
		t.Errorf("temp = %v, want 18", got[0].TempC)
	}
	if got[0].Condition != forecast.CondClear {
		t.Errorf("condition = %q, want clear", got[0].Condition)
	}
	if got[0].PrecipProb == nil || *got[0].PrecipProb != 20 {
		t.Errorf("precip_prob = %v, want 20", got[0].PrecipProb)
	}
	if got[0].CloudPct == nil || *got[0].CloudPct != 40 {
		t.Errorf("cloud_pct = %v, want 40", got[0].CloudPct)
	}
	if got[1].PressureHpa != nil || got[1].PrecipProb != nil || got[1].CloudPct != nil {
		t.Errorf("absent fields should be nil, got pres=%v prob=%v cloud=%v", got[1].PressureHpa, got[1].PrecipProb, got[1].CloudPct)
	}

	// Re-upsert the same hour with a revised value -> REPLACE, not duplicate.
	if err := s.UpsertForecast("openmeteo", []forecast.Point{{TS: base, TempC: f(19)}}, fetched.Add(time.Hour)); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	got, _, _ = s.Forecast("openmeteo", base, base.Add(48*time.Hour))
	if len(got) != 2 {
		t.Fatalf("after replace got %d points, want 2", len(got))
	}
	if got[0].TempC == nil || *got[0].TempC != 19 {
		t.Errorf("revised temp = %v, want 19", got[0].TempC)
	}

	// Range filter excludes out-of-window hours.
	windowed, _, _ := s.Forecast("openmeteo", base.Add(30*time.Minute), base.Add(48*time.Hour))
	if len(windowed) != 1 {
		t.Errorf("windowed got %d, want 1", len(windowed))
	}

	// Prune drops past hours.
	n, err := s.PruneForecast(base.Add(30 * time.Minute))
	if err != nil {
		t.Fatalf("PruneForecast: %v", err)
	}
	if n != 1 {
		t.Errorf("pruned %d, want 1", n)
	}
}
