package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/tlugger/wtwlt/server/internal/model"
	"github.com/tlugger/wtwlt/server/internal/store"
)

func newServer(t *testing.T) (*store.Store, http.Handler) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st, New(st).Handler()
}

func do(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func decode(t *testing.T, rr *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), v); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, rr.Body.String())
	}
}

func fp(v float64) *float64 { return &v }

func ts(s string) time.Time {
	tm, _ := time.Parse(time.RFC3339, s)
	return tm
}

func TestHealthz(t *testing.T) {
	_, h := newServer(t)
	rr := do(t, h, "/healthz")
	if rr.Code != 200 || rr.Body.String() != "ok\n" {
		t.Errorf("healthz = %d %q", rr.Code, rr.Body.String())
	}
}

func fullReading() model.Reading {
	return model.Reading{
		StationID:   "wtwlt-01",
		TS:          "2026-06-16T12:00:00Z",
		IntervalS:   60,
		TempC:       fp(21.4),
		HumidityPct: fp(58.2),
		PressureHpa: fp(1013.2),
		UVIndex:     fp(3.1),
		SoilPct:     fp(42.0),
		Wind:        model.Wind{AvgMPS: 2.5, GustMPS: 8.0, DirDeg: 270, DirCardinal: "W"},
		RainMM:      0.5,
		Diagnostics: model.Diagnostics{BatteryV: fp(3.92), RSSIDbm: -67, FWVersion: "1.0.0"},
	}
}

func TestCurrentMetric(t *testing.T) {
	st, h := newServer(t)
	if err := st.InsertReading(fullReading(), ts("2026-06-16T12:00:00Z"), ts("2026-06-16T12:00:00Z")); err != nil {
		t.Fatal(err)
	}
	rr := do(t, h, "/api/current")
	if rr.Code != 200 {
		t.Fatalf("status %d", rr.Code)
	}
	var dto currentDTO
	decode(t, rr, &dto)

	if dto.UnitSystem != "metric" || dto.Units.Temp != "°C" {
		t.Errorf("units = %s / %s", dto.UnitSystem, dto.Units.Temp)
	}
	if dto.Temp == nil || *dto.Temp != 21.4 {
		t.Errorf("temp = %v", dto.Temp)
	}
	if dto.Pressure == nil || *dto.Pressure != 1013.2 {
		t.Errorf("pressure = %v", dto.Pressure)
	}
	if dto.Wind.Avg != 2.5 || dto.Wind.Gust != 8.0 || dto.Wind.DirCardinal != "W" {
		t.Errorf("wind = %+v", dto.Wind)
	}
	if dto.Rain != 0.5 {
		t.Errorf("rain = %v", dto.Rain)
	}
	if dto.BatteryV == nil || *dto.BatteryV != 3.92 {
		t.Errorf("battery = %v", dto.BatteryV) // volts: never converted
	}
}

func TestCurrentImperial(t *testing.T) {
	st, h := newServer(t)
	if err := st.InsertReading(fullReading(), ts("2026-06-16T12:00:00Z"), ts("2026-06-16T12:00:00Z")); err != nil {
		t.Fatal(err)
	}
	rr := do(t, h, "/api/current?units=imperial")
	var dto currentDTO
	decode(t, rr, &dto)

	if dto.UnitSystem != "imperial" || dto.Units.Temp != "°F" {
		t.Errorf("units = %s / %s", dto.UnitSystem, dto.Units.Temp)
	}
	if dto.Temp == nil || *dto.Temp != 70.5 { // 21.4C -> 70.52 -> 70.5
		t.Errorf("temp °F = %v, want 70.5", dto.Temp)
	}
	if dto.Pressure == nil || *dto.Pressure != 29.92 { // 1013.2 hPa -> inHg
		t.Errorf("pressure inHg = %v, want 29.92", dto.Pressure)
	}
	if dto.Wind.Gust != 17.9 { // 8 m/s -> mph
		t.Errorf("gust mph = %v, want 17.9", dto.Wind.Gust)
	}
	if dto.Humidity == nil || *dto.Humidity != 58.2 {
		t.Errorf("humidity should pass through unchanged, got %v", dto.Humidity)
	}
}

func TestCurrentNotFound(t *testing.T) {
	_, h := newServer(t)
	if rr := do(t, h, "/api/current?station=ghost"); rr.Code != 404 {
		t.Errorf("want 404, got %d", rr.Code)
	}
}

func seedHistory(t *testing.T, st *store.Store) {
	t.Helper()
	rows := []struct {
		ts          string
		temp, gust, rain float64
	}{
		{"2026-06-16T12:00:00Z", 20, 5, 0.2},
		{"2026-06-16T12:30:00Z", 22, 9, 0.3},
		{"2026-06-16T13:00:00Z", 24, 4, 0.1},
	}
	for _, row := range rows {
		r := model.Reading{StationID: "wtwlt-01", TempC: fp(row.temp), RainMM: row.rain,
			Wind: model.Wind{GustMPS: row.gust, DirCardinal: "N"}}
		if err := st.InsertReading(r, ts(row.ts), ts(row.ts)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHistoryHourly(t *testing.T) {
	st, h := newServer(t)
	seedHistory(t, st)

	rr := do(t, h, "/api/history?bucket=hour&from=2026-06-16T11:00:00Z&to=2026-06-16T14:00:00Z")
	if rr.Code != 200 {
		t.Fatalf("status %d (%s)", rr.Code, rr.Body.String())
	}
	var resp historyResp
	decode(t, rr, &resp)

	if resp.Bucket != "hour" || resp.UnitSystem != "metric" {
		t.Errorf("meta = %+v", resp)
	}
	if len(resp.Points) != 2 {
		t.Fatalf("want 2 points, got %d", len(resp.Points))
	}
	h12 := resp.Points[0]
	if h12.Count != 2 || h12.Temp == nil || *h12.Temp != 21 {
		t.Errorf("hour12 = %+v", h12)
	}
	if h12.WindGust == nil || *h12.WindGust != 9 {
		t.Errorf("hour12 gust = %v, want 9", h12.WindGust)
	}
}

func TestHistoryInvalidBucket(t *testing.T) {
	_, h := newServer(t)
	if rr := do(t, h, "/api/history?bucket=week"); rr.Code != 400 {
		t.Errorf("want 400, got %d", rr.Code)
	}
}

func TestHistoryInvalidTime(t *testing.T) {
	_, h := newServer(t)
	if rr := do(t, h, "/api/history?from=not-a-time"); rr.Code != 400 {
		t.Errorf("want 400, got %d", rr.Code)
	}
}

func TestSummary(t *testing.T) {
	st, h := newServer(t)
	for _, row := range []struct {
		ts         string
		temp, gust, rain float64
	}{
		{"2026-06-16T12:00:00Z", 18, 5, 0.2},
		{"2026-06-16T13:00:00Z", 26, 12, 0.3},
	} {
		r := model.Reading{StationID: "wtwlt-01", TempC: fp(row.temp), RainMM: row.rain,
			Wind: model.Wind{GustMPS: row.gust, DirCardinal: "N"}}
		if err := st.InsertReading(r, ts(row.ts), ts(row.ts)); err != nil {
			t.Fatal(err)
		}
	}

	rr := do(t, h, "/api/summary?from=2026-06-16T11:00:00Z&to=2026-06-16T14:00:00Z")
	var resp summaryResp
	decode(t, rr, &resp)

	if resp.Count != 2 {
		t.Errorf("count = %d", resp.Count)
	}
	if resp.Temp.Min == nil || *resp.Temp.Min != 18 || resp.Temp.Max == nil || *resp.Temp.Max != 26 {
		t.Errorf("temp range = %+v", resp.Temp)
	}
	if resp.Temp.Avg == nil || *resp.Temp.Avg != 22 {
		t.Errorf("temp avg = %v", resp.Temp.Avg)
	}
	if resp.Wind.GustMax == nil || *resp.Wind.GustMax != 12 {
		t.Errorf("gust max = %v", resp.Wind.GustMax)
	}
	if resp.RainTotal != 0.5 {
		t.Errorf("rain total = %v", resp.RainTotal)
	}
}

func TestLightning(t *testing.T) {
	st, h := newServer(t)
	for i, km := range []int{5, 10, 15} {
		l := model.Lightning{StationID: "wtwlt-01", Event: "strike", DistanceKm: km, Energy: int64(1000 * (i + 1))}
		tm := ts("2026-06-16T12:00:00Z").Add(time.Duration(i) * time.Minute)
		if err := st.InsertLightning(l, tm, tm); err != nil {
			t.Fatal(err)
		}
	}

	rr := do(t, h, "/api/lightning?from=2026-06-16T11:00:00Z&to=2026-06-16T13:00:00Z&units=imperial")
	var resp lightningResp
	decode(t, rr, &resp)

	if len(resp.Events) != 3 {
		t.Fatalf("want 3 events, got %d", len(resp.Events))
	}
	if resp.Events[0].Distance != 9.3 { // newest first: 15km -> 9.3mi
		t.Errorf("newest distance = %v mi, want 9.3", resp.Events[0].Distance)
	}

	rrLim := do(t, h, "/api/lightning?from=2026-06-16T11:00:00Z&to=2026-06-16T13:00:00Z&limit=2")
	var lim lightningResp
	decode(t, rrLim, &lim)
	if len(lim.Events) != 2 {
		t.Errorf("limit=2 returned %d", len(lim.Events))
	}
}

func TestStations(t *testing.T) {
	st, h := newServer(t)
	if err := st.UpsertStatus(model.Status{StationID: "wtwlt-01", Online: true, IP: "10.0.0.5"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	rr := do(t, h, "/api/stations")
	var stations []store.StationStatus
	decode(t, rr, &stations)
	if len(stations) != 1 || !stations[0].Online {
		t.Errorf("stations = %+v", stations)
	}
}
