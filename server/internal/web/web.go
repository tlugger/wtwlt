// Package web serves the JSON API (and, later, the dashboard) from the store.
//
// Values are stored metric and converted to the caller's `units` (metric|imperial)
// at response time. Dashboard endpoints use unit-neutral field names plus a
// `units` descriptor object so a metric/imperial toggle is unambiguous.
package web

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/samber/lo"

	"github.com/tlugger/wtwlt/server/internal/forecast"
	"github.com/tlugger/wtwlt/server/internal/store"
	"github.com/tlugger/wtwlt/server/internal/units"
)

//go:embed dashboard.html
var dashboardHTML []byte

const defaultStation = "wtwlt-01"

type Server struct {
	store *store.Store

	mu       sync.RWMutex
	location string // coarse place label (e.g. "Thornton, Colorado"); resolved async
}

func New(st *store.Store) *Server { return &Server{store: st} }

// SetLocation records the coarse forecast-location label shown on the dashboard.
// Exact coordinates are never exposed to the client.
func (s *Server) SetLocation(loc string) {
	s.mu.Lock()
	s.location = loc
	s.mu.Unlock()
}

func (s *Server) getLocation() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.location
}

// Handler returns the HTTP routes (stdlib mux; Go 1.22+ method+path patterns).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.dashboard)
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /api/current", s.current)
	mux.HandleFunc("GET /api/history", s.history)
	mux.HandleFunc("GET /api/forecast", s.forecast)
	mux.HandleFunc("GET /api/summary", s.summary)
	mux.HandleFunc("GET /api/lightning", s.lightning)
	mux.HandleFunc("GET /api/stations", s.stations)
	return mux
}

func (s *Server) dashboard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(dashboardHTML)
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("ok\n"))
}

// --- DTOs -------------------------------------------------------------------

type windDTO struct {
	Avg         float64 `json:"avg"`
	Gust        float64 `json:"gust"`
	DirDeg      float64 `json:"dir_deg"`
	DirCardinal string  `json:"dir_cardinal"`
}

type currentDTO struct {
	StationID  string       `json:"station_id"`
	TS         string       `json:"ts"`
	Temp       *float64     `json:"temp"`
	Humidity   *float64     `json:"humidity"`
	Pressure   *float64     `json:"pressure"`
	Wind       windDTO      `json:"wind"`
	Rain       float64      `json:"rain"`
	Soil       *float64     `json:"soil"`
	BatteryV   *float64     `json:"battery_v"`
	RSSIDbm    int          `json:"rssi_dbm"`
	UnitSystem string       `json:"unit_system"`
	Units      units.Labels `json:"units"`
}

type historyPoint struct {
	Bucket   string   `json:"bucket"`
	Count    int      `json:"count"`
	Temp     *float64 `json:"temp"`
	Humidity *float64 `json:"humidity"`
	Pressure *float64 `json:"pressure"`
	UV       *float64 `json:"uv"`
	WindAvg  *float64 `json:"wind_avg"`
	WindGust *float64 `json:"wind_gust"`
	Rain     float64  `json:"rain"`
	Soil     *float64 `json:"soil"`
}

type historyResp struct {
	Station    string         `json:"station"`
	Bucket     string         `json:"bucket"`
	From       string         `json:"from"`
	To         string         `json:"to"`
	UnitSystem string         `json:"unit_system"`
	Units      units.Labels   `json:"units"`
	Points     []historyPoint `json:"points"`
}

type forecastPoint struct {
	TS         string   `json:"ts"`
	Temp       *float64 `json:"temp"`
	Humidity   *float64 `json:"humidity"`
	Pressure   *float64 `json:"pressure"`
	Precip     *float64 `json:"precip"`
	PrecipProb *float64 `json:"precip_prob"`
	Cloud      *float64 `json:"cloud_pct"`
	UV         *float64 `json:"uv"`
	WindAvg    *float64 `json:"wind_avg"`
	WindDir    *float64 `json:"wind_dir"`
	Condition  string   `json:"condition"`
}

type forecastResp struct {
	Station    string          `json:"station"`
	Source     string          `json:"source"`
	Location   string          `json:"location"`
	From       string          `json:"from"`
	To         string          `json:"to"`
	UnitSystem string          `json:"unit_system"`
	Units      units.Labels    `json:"units"`
	Points     []forecastPoint `json:"points"`
}

type rangeStat struct {
	Min *float64 `json:"min"`
	Max *float64 `json:"max"`
	Avg *float64 `json:"avg"`
}

type summaryResp struct {
	Station    string       `json:"station"`
	From       string       `json:"from"`
	To         string       `json:"to"`
	Count      int          `json:"count"`
	UnitSystem string       `json:"unit_system"`
	Units      units.Labels `json:"units"`
	Temp       rangeStat    `json:"temp"`
	Humidity   struct {
		Avg *float64 `json:"avg"`
	} `json:"humidity"`
	Pressure rangeStat `json:"pressure"`
	Wind     struct {
		Avg     *float64 `json:"avg"`
		GustMax *float64 `json:"gust_max"`
	} `json:"wind"`
	RainTotal float64 `json:"rain_total"`
}

type lightningEventDTO struct {
	TS       string  `json:"ts"`
	Event    string  `json:"event"`
	Distance float64 `json:"distance"`
	Energy   int64   `json:"energy"`
}

type lightningResp struct {
	Station    string              `json:"station"`
	From       string              `json:"from"`
	To         string              `json:"to"`
	UnitSystem string              `json:"unit_system"`
	Units      units.Labels        `json:"units"`
	Events     []lightningEventDTO `json:"events"`
}

// --- handlers ---------------------------------------------------------------

// GET /api/current?station=&units= — latest reading for a station.
func (s *Server) current(w http.ResponseWriter, r *http.Request) {
	sys := units.Parse(r.URL.Query().Get("units"))
	reading, _, err := s.store.LatestReading(station(r))
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "no readings for station", http.StatusNotFound)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, currentDTO{
		StationID: reading.StationID,
		TS:        reading.TS,
		Temp:      sys.Temp(reading.TempC),
		Humidity:  sys.Pct(reading.HumidityPct),
		Pressure:  sys.Pressure(reading.PressureHpa),
		Wind: windDTO{
			Avg:         sys.SpeedV(reading.Wind.AvgMPS),
			Gust:        sys.SpeedV(reading.Wind.GustMPS),
			DirDeg:      reading.Wind.DirDeg,
			DirCardinal: reading.Wind.DirCardinal,
		},
		Rain:       sys.RainV(reading.RainMM),
		Soil:       reading.SoilPct,
		BatteryV:   reading.Diagnostics.BatteryV,
		RSSIDbm:    reading.Diagnostics.RSSIDbm,
		UnitSystem: sys.Name(),
		Units:      sys.Labels(),
	})
}

// GET /api/history?station=&from=&to=&bucket=raw|hour|day&units=
func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	sys := units.Parse(r.URL.Query().Get("units"))
	from, to, err := timeRange(r)
	if err != nil {
		badRequest(w, err)
		return
	}
	bucket := r.URL.Query().Get("bucket")
	if bucket == "" {
		bucket = "raw"
	}
	buckets, err := s.store.History(station(r), from, to, bucket)
	if err != nil {
		// Invalid bucket is the only user-caused error here.
		badRequest(w, err)
		return
	}

	resp := historyResp{
		Station: station(r), Bucket: bucket,
		From: from.Format(time.RFC3339), To: to.Format(time.RFC3339),
		UnitSystem: sys.Name(), Units: sys.Labels(),
		Points: lo.Map(buckets, func(b store.HistoryBucket, _ int) historyPoint {
			return historyPoint{
				Bucket:   b.Bucket.Format(time.RFC3339),
				Count:    b.Count,
				Temp:     sys.Temp(b.TempC),
				Humidity: sys.Pct(b.HumidityPct),
				Pressure: sys.Pressure(b.PressureHpa),
				UV:       sys.UV(b.UVIndex),
				WindAvg:  sys.Speed(b.WindAvgMPS),
				WindGust: sys.Speed(b.WindGustMPS),
				Rain:     sys.RainV(b.RainMM),
				Soil:     sys.Pct(b.SoilPct),
			}
		}),
	}
	writeJSON(w, resp)
}

// GET /api/forecast?station=&units=&source= — stored forecast for the coming
// week (the chart overlay clamps to 48h client-side; the tiles use the rest).
func (s *Server) forecast(w http.ResponseWriter, r *http.Request) {
	sys := units.Parse(r.URL.Query().Get("units"))
	now := time.Now().UTC()
	from, to := now, now.Add(7*24*time.Hour)
	pts, source, err := s.store.Forecast(r.URL.Query().Get("source"), from, to)
	if err != nil {
		serverError(w, err)
		return
	}
	resp := forecastResp{
		Station: station(r), Source: source,
		Location: s.getLocation(),
		From:     from.Format(time.RFC3339), To: to.Format(time.RFC3339),
		UnitSystem: sys.Name(), Units: sys.Labels(),
		Points: lo.Map(pts, func(p forecast.Point, _ int) forecastPoint {
			return forecastPoint{
				TS:         p.TS.Format(time.RFC3339),
				Temp:       sys.Temp(p.TempC),
				Humidity:   sys.Pct(p.HumidityPct),
				Pressure:   sys.Pressure(p.PressureHpa),
				Precip:     sys.Rain(p.PrecipMm),
				PrecipProb: sys.Pct(p.PrecipProb),
				Cloud:      sys.Pct(p.CloudPct),
				UV:         sys.UV(p.UVIndex),
				WindAvg:    sys.Speed(p.WindMps),
				WindDir:    p.WindDirDeg,
				Condition:  p.Condition,
			}
		}),
	}
	if resp.Points == nil {
		resp.Points = []forecastPoint{}
	}
	writeJSON(w, resp)
}

// GET /api/summary?station=&from=&to=&units=
func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	sys := units.Parse(r.URL.Query().Get("units"))
	from, to, err := timeRange(r)
	if err != nil {
		badRequest(w, err)
		return
	}
	sum, err := s.store.Summarize(station(r), from, to)
	if err != nil {
		serverError(w, err)
		return
	}

	resp := summaryResp{
		Station: station(r),
		From:    from.Format(time.RFC3339), To: to.Format(time.RFC3339),
		Count:      sum.Count,
		UnitSystem: sys.Name(), Units: sys.Labels(),
		RainTotal: sys.RainV(sum.RainTotalMM),
	}
	resp.Temp = rangeStat{Min: sys.Temp(sum.TempMinC), Max: sys.Temp(sum.TempMaxC), Avg: sys.Temp(sum.TempAvgC)}
	resp.Humidity.Avg = sys.Pct(sum.HumidityAvgPct)
	resp.Pressure = rangeStat{Min: sys.Pressure(sum.PressureMinHpa), Max: sys.Pressure(sum.PressureMaxHpa), Avg: sys.Pressure(sum.PressureAvgHpa)}
	resp.Wind.Avg = sys.Speed(sum.WindAvgMPS)
	resp.Wind.GustMax = sys.Speed(sum.WindGustMaxMPS)
	writeJSON(w, resp)
}

// GET /api/lightning?station=&from=&to=&limit=&units=
func (s *Server) lightning(w http.ResponseWriter, r *http.Request) {
	sys := units.Parse(r.URL.Query().Get("units"))
	from, to, err := timeRange(r)
	if err != nil {
		badRequest(w, err)
		return
	}
	events, err := s.store.LightningEvents(station(r), from, to, limitParam(r, 100, 1000))
	if err != nil {
		serverError(w, err)
		return
	}

	resp := lightningResp{
		Station: station(r),
		From:    from.Format(time.RFC3339), To: to.Format(time.RFC3339),
		UnitSystem: sys.Name(), Units: sys.Labels(),
		Events: lo.Map(events, func(e store.LightningEvent, _ int) lightningEventDTO {
			return lightningEventDTO{
				TS:       e.TS.Format(time.RFC3339),
				Event:    e.Event,
				Distance: sys.Distance(e.DistanceKm),
				Energy:   e.Energy,
			}
		}),
	}
	writeJSON(w, resp)
}

// GET /api/stations — status of all known stations.
func (s *Server) stations(w http.ResponseWriter, _ *http.Request) {
	stations, err := s.store.Stations()
	if err != nil {
		serverError(w, err)
		return
	}
	if stations == nil {
		stations = []store.StationStatus{}
	}
	writeJSON(w, stations)
}

// --- request params ---------------------------------------------------------

func station(r *http.Request) string {
	if s := r.URL.Query().Get("station"); s != "" {
		return s
	}
	return defaultStation
}

// timeRange parses from/to (RFC3339), defaulting to the last 24h.
func timeRange(r *http.Request) (from, to time.Time, err error) {
	q := r.URL.Query()
	to = time.Now().UTC()
	if v := q.Get("to"); v != "" {
		to, err = time.Parse(time.RFC3339, v)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid 'to' (want RFC3339): %w", err)
		}
	}
	from = to.Add(-24 * time.Hour)
	if v := q.Get("from"); v != "" {
		from, err = time.Parse(time.RFC3339, v)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid 'from' (want RFC3339): %w", err)
		}
	}
	return from.UTC(), to.UTC(), nil
}

func limitParam(r *http.Request, def, max int) int {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// --- responses --------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func badRequest(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func serverError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
