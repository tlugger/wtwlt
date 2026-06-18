// Package store persists readings/lightning/status to SQLite.
//
// Uses modernc.org/sqlite (pure Go, cgo-free) so the server cross-compiles to
// the Pi with a plain `GOOS=linux GOARCH=arm64 go build`. WAL mode lets the
// HTTP read path run concurrently with the MQTT ingest write path.
package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/tlugger/wtwlt/server/internal/model"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS readings (
    id                INTEGER PRIMARY KEY,
    station_id        TEXT NOT NULL,
    ts                TEXT NOT NULL,
    received_at       TEXT NOT NULL,
    interval_s        INTEGER,
    temp_c            REAL,
    humidity_pct      REAL,
    pressure_hpa      REAL,
    uv_index          REAL,
    wind_avg_mps      REAL,
    wind_gust_mps     REAL,
    wind_dir_deg      REAL,
    wind_dir_cardinal TEXT,
    rain_mm           REAL,
    soil_moisture_pct REAL,
    battery_v         REAL,
    rssi_dbm          INTEGER,
    uptime_s          INTEGER,
    fw_version        TEXT
);
CREATE INDEX IF NOT EXISTS idx_readings_station_ts ON readings(station_id, ts);

CREATE TABLE IF NOT EXISTS lightning (
    id          INTEGER PRIMARY KEY,
    station_id  TEXT NOT NULL,
    ts          TEXT NOT NULL,
    received_at TEXT NOT NULL,
    event       TEXT,
    distance_km INTEGER,
    energy      INTEGER
);
CREATE INDEX IF NOT EXISTS idx_lightning_station_ts ON lightning(station_id, ts);

CREATE TABLE IF NOT EXISTS station_status (
    station_id  TEXT PRIMARY KEY,
    online      INTEGER NOT NULL,
    fw_version  TEXT,
    ip          TEXT,
    boot_ts     TEXT,
    updated_at  TEXT NOT NULL
);
`

// Open opens (and migrates) the SQLite database at path. Use ":memory:" for tests.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func isoUTC(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// InsertReading stores one aggregated reading. tsUTC/received are pre-resolved
// (see model.EventTime).
func (s *Store) InsertReading(r model.Reading, tsUTC, received time.Time) error {
	_, err := s.db.Exec(`
		INSERT INTO readings (
			station_id, ts, received_at, interval_s,
			temp_c, humidity_pct, pressure_hpa, uv_index,
			wind_avg_mps, wind_gust_mps, wind_dir_deg, wind_dir_cardinal,
			rain_mm, soil_moisture_pct,
			battery_v, rssi_dbm, uptime_s, fw_version
		) VALUES (?,?,?,?, ?,?,?,?, ?,?,?,?, ?,?, ?,?,?,?)`,
		r.StationID, isoUTC(tsUTC), isoUTC(received), r.IntervalS,
		fptr(r.TempC), fptr(r.HumidityPct), fptr(r.PressureHpa), fptr(r.UVIndex),
		r.Wind.AvgMPS, r.Wind.GustMPS, r.Wind.DirDeg, r.Wind.DirCardinal,
		r.RainMM, fptr(r.SoilPct),
		fptr(r.Diagnostics.BatteryV), r.Diagnostics.RSSIDbm, r.Diagnostics.UptimeS, r.Diagnostics.FWVersion,
	)
	return err
}

// InsertLightning stores one lightning event.
func (s *Store) InsertLightning(l model.Lightning, tsUTC, received time.Time) error {
	_, err := s.db.Exec(`
		INSERT INTO lightning (station_id, ts, received_at, event, distance_km, energy)
		VALUES (?,?,?,?,?,?)`,
		l.StationID, isoUTC(tsUTC), isoUTC(received), l.Event, l.DistanceKm, l.Energy)
	return err
}

// UpsertStatus records the latest station status.
func (s *Store) UpsertStatus(st model.Status, received time.Time) error {
	_, err := s.db.Exec(`
		INSERT INTO station_status (station_id, online, fw_version, ip, boot_ts, updated_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(station_id) DO UPDATE SET
			online=excluded.online, fw_version=excluded.fw_version,
			ip=excluded.ip, boot_ts=excluded.boot_ts, updated_at=excluded.updated_at`,
		st.StationID, boolInt(st.Online), st.FWVersion, st.IP, st.BootTS, isoUTC(received))
	return err
}

// LatestReading returns the most recent reading for a station (by ts).
func (s *Store) LatestReading(stationID string) (model.Reading, time.Time, error) {
	row := s.db.QueryRow(`
		SELECT station_id, ts, interval_s,
		       temp_c, humidity_pct, pressure_hpa, uv_index,
		       wind_avg_mps, wind_gust_mps, wind_dir_deg, wind_dir_cardinal,
		       rain_mm, soil_moisture_pct,
		       battery_v, rssi_dbm, uptime_s, fw_version
		FROM readings WHERE station_id=? ORDER BY ts DESC LIMIT 1`, stationID)

	var (
		r    model.Reading
		tsS  string
		temp, hum, pres, uv, soil, batt sql.NullFloat64
	)
	err := row.Scan(
		&r.StationID, &tsS, &r.IntervalS,
		&temp, &hum, &pres, &uv,
		&r.Wind.AvgMPS, &r.Wind.GustMPS, &r.Wind.DirDeg, &r.Wind.DirCardinal,
		&r.RainMM, &soil,
		&batt, &r.Diagnostics.RSSIDbm, &r.Diagnostics.UptimeS, &r.Diagnostics.FWVersion,
	)
	if err != nil {
		return r, time.Time{}, err
	}
	r.TS = tsS
	r.TempC = nullF(temp)
	r.HumidityPct = nullF(hum)
	r.PressureHpa = nullF(pres)
	r.UVIndex = nullF(uv)
	r.SoilPct = nullF(soil)
	r.Diagnostics.BatteryV = nullF(batt)
	ts, _ := time.Parse(time.RFC3339, tsS)
	return r, ts, nil
}

// StationStatus is one row of station_status.
type StationStatus struct {
	StationID string `json:"station_id"`
	Online    bool   `json:"online"`
	FWVersion string `json:"fw_version"`
	IP        string `json:"ip"`
	BootTS    string `json:"boot_ts"`
	UpdatedAt string `json:"updated_at"`
}

// Stations returns the status of every known station.
func (s *Store) Stations() ([]StationStatus, error) {
	rows, err := s.db.Query(`
		SELECT station_id, online, fw_version, ip, boot_ts, updated_at
		FROM station_status ORDER BY station_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []StationStatus
	for rows.Next() {
		var ss StationStatus
		var online int
		if err := rows.Scan(&ss.StationID, &online, &ss.FWVersion, &ss.IP, &ss.BootTS, &ss.UpdatedAt); err != nil {
			return nil, err
		}
		ss.Online = online != 0
		out = append(out, ss)
	}
	return out, rows.Err()
}

// HistoryBucket is one time bucket of aggregated readings. Nullable averages are
// nil when every reading in the bucket had that sensor absent.
type HistoryBucket struct {
	Bucket      time.Time `json:"-"`
	Count       int
	TempC       *float64
	HumidityPct *float64
	PressureHpa *float64
	UVIndex     *float64
	WindAvgMPS  *float64
	WindGustMPS *float64 // peak gust in the bucket
	RainMM      float64  // accumulated in the bucket
	SoilPct     *float64
}

// bucketExpr returns a SQL expression that truncates the RFC3339 `ts` column to
// the requested granularity using substr (robust across SQLite versions).
func bucketExpr(bucket string) (string, error) {
	switch bucket {
	case "", "raw":
		return "ts", nil
	case "hour":
		return "substr(ts,1,13) || ':00:00Z'", nil
	case "day":
		return "substr(ts,1,10) || 'T00:00:00Z'", nil
	default:
		return "", fmt.Errorf("invalid bucket %q (want raw|hour|day)", bucket)
	}
}

// History returns aggregated readings for [from,to) bucketed by granularity.
func (s *Store) History(stationID string, from, to time.Time, bucket string) ([]HistoryBucket, error) {
	expr, err := bucketExpr(bucket)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`
		SELECT %s AS bucket, COUNT(*),
		       AVG(temp_c), AVG(humidity_pct), AVG(pressure_hpa), AVG(uv_index),
		       AVG(wind_avg_mps), MAX(wind_gust_mps), SUM(rain_mm), AVG(soil_moisture_pct)
		FROM readings
		WHERE station_id=? AND ts>=? AND ts<?
		GROUP BY bucket ORDER BY bucket`, expr)

	rows, err := s.db.Query(q, stationID, isoUTC(from), isoUTC(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []HistoryBucket
	for rows.Next() {
		var (
			b                                       HistoryBucket
			bucketS                                 string
			temp, hum, pres, uv, wavg, wgust, soil  sql.NullFloat64
			rain                                    sql.NullFloat64
		)
		if err := rows.Scan(&bucketS, &b.Count, &temp, &hum, &pres, &uv, &wavg, &wgust, &rain, &soil); err != nil {
			return nil, err
		}
		b.Bucket, _ = time.Parse(time.RFC3339, bucketS)
		b.TempC, b.HumidityPct, b.PressureHpa, b.UVIndex = nullF(temp), nullF(hum), nullF(pres), nullF(uv)
		b.WindAvgMPS, b.WindGustMPS, b.SoilPct = nullF(wavg), nullF(wgust), nullF(soil)
		if rain.Valid {
			b.RainMM = rain.Float64
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Summary is min/max/avg statistics over a time range.
type Summary struct {
	Count          int
	TempMinC       *float64
	TempMaxC       *float64
	TempAvgC       *float64
	HumidityAvgPct *float64
	PressureMinHpa *float64
	PressureMaxHpa *float64
	PressureAvgHpa *float64
	WindAvgMPS     *float64
	WindGustMaxMPS *float64
	RainTotalMM    float64
}

// Summarize returns aggregate stats for [from,to).
func (s *Store) Summarize(stationID string, from, to time.Time) (Summary, error) {
	row := s.db.QueryRow(`
		SELECT COUNT(*),
		       MIN(temp_c), MAX(temp_c), AVG(temp_c),
		       AVG(humidity_pct),
		       MIN(pressure_hpa), MAX(pressure_hpa), AVG(pressure_hpa),
		       AVG(wind_avg_mps), MAX(wind_gust_mps),
		       SUM(rain_mm)
		FROM readings WHERE station_id=? AND ts>=? AND ts<?`,
		stationID, isoUTC(from), isoUTC(to))

	var (
		sum                                        Summary
		tmin, tmax, tavg, havg                     sql.NullFloat64
		pmin, pmax, pavg, wavg, wgust, rain        sql.NullFloat64
	)
	if err := row.Scan(&sum.Count, &tmin, &tmax, &tavg, &havg, &pmin, &pmax, &pavg, &wavg, &wgust, &rain); err != nil {
		return sum, err
	}
	sum.TempMinC, sum.TempMaxC, sum.TempAvgC = nullF(tmin), nullF(tmax), nullF(tavg)
	sum.HumidityAvgPct = nullF(havg)
	sum.PressureMinHpa, sum.PressureMaxHpa, sum.PressureAvgHpa = nullF(pmin), nullF(pmax), nullF(pavg)
	sum.WindAvgMPS, sum.WindGustMaxMPS = nullF(wavg), nullF(wgust)
	if rain.Valid {
		sum.RainTotalMM = rain.Float64
	}
	return sum, nil
}

// LightningEvent is one stored strike (or disturber/noise) event.
type LightningEvent struct {
	StationID  string
	TS         time.Time
	Event      string
	DistanceKm int
	Energy     int64
}

// LightningEvents returns events in [from,to), newest first, capped at limit.
func (s *Store) LightningEvents(stationID string, from, to time.Time, limit int) ([]LightningEvent, error) {
	rows, err := s.db.Query(`
		SELECT station_id, ts, event, distance_km, energy
		FROM lightning
		WHERE station_id=? AND ts>=? AND ts<?
		ORDER BY ts DESC LIMIT ?`,
		stationID, isoUTC(from), isoUTC(to), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LightningEvent
	for rows.Next() {
		var (
			e   LightningEvent
			tsS string
		)
		if err := rows.Scan(&e.StationID, &tsS, &e.Event, &e.DistanceKm, &e.Energy); err != nil {
			return nil, err
		}
		e.TS, _ = time.Parse(time.RFC3339, tsS)
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- helpers: map *float64 <-> SQL NULL ---

func fptr(p *float64) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

func nullF(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
