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

	"github.com/tlugger/wtwlt/server/internal/forecast"
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

-- Downsampled rollups (recomputed from raw; survive raw pruning). bucket is the
-- RFC3339 start of the hour/day. Same columns for both granularities.
CREATE TABLE IF NOT EXISTS readings_hourly (
    station_id    TEXT NOT NULL,
    bucket        TEXT NOT NULL,
    count         INTEGER NOT NULL,
    temp_avg REAL, temp_min REAL, temp_max REAL,
    humidity_avg REAL,
    pressure_avg REAL, pressure_min REAL, pressure_max REAL,
    uv_avg REAL, uv_max REAL,
    wind_avg REAL, wind_gust_max REAL,
    rain_sum REAL,
    soil_avg REAL,
    PRIMARY KEY (station_id, bucket)
);
CREATE TABLE IF NOT EXISTS readings_daily (
    station_id    TEXT NOT NULL,
    bucket        TEXT NOT NULL,
    count         INTEGER NOT NULL,
    temp_avg REAL, temp_min REAL, temp_max REAL,
    humidity_avg REAL,
    pressure_avg REAL, pressure_min REAL, pressure_max REAL,
    uv_avg REAL, uv_max REAL,
    wind_avg REAL, wind_gust_max REAL,
    rain_sum REAL,
    soil_avg REAL,
    PRIMARY KEY (station_id, bucket)
);

-- Fetched forecast (NOT measured): one row per source+hour, in metric/SI.
-- INSERT OR REPLACE on refresh keeps the latest projection for each hour.
CREATE TABLE IF NOT EXISTS forecast (
    source       TEXT NOT NULL,
    ts           TEXT NOT NULL,
    fetched_at   TEXT NOT NULL,
    temp_c       REAL,
    humidity_pct REAL,
    pressure_hpa REAL,
    precip_mm    REAL,
    precip_prob  REAL,
    cloud_pct    REAL,
    wind_mps     REAL,
    wind_dir_deg REAL,
    condition    TEXT,
    PRIMARY KEY (source, ts)
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
	// Additive migrations for columns added after the forecast table shipped.
	// The error on an already-present column is expected and ignored.
	db.Exec(`ALTER TABLE forecast ADD COLUMN condition TEXT`)
	db.Exec(`ALTER TABLE forecast ADD COLUMN precip_prob REAL`)
	db.Exec(`ALTER TABLE forecast ADD COLUMN cloud_pct REAL`)
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
		r                               model.Reading
		tsS                             string
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

// History returns aggregated readings for [from,to). `raw` aggregates live raw
// readings (one point per reading); `hour`/`day` read the precomputed rollup
// tables (fast for long ranges, and they survive raw pruning).
func (s *Store) History(stationID string, from, to time.Time, bucket string) ([]HistoryBucket, error) {
	switch bucket {
	case "", "raw":
		return s.historyRaw(stationID, from, to)
	case "hour":
		// floor `from` to the bucket start so a partially-covered first bucket is included
		return s.historyRollup("readings_hourly", stationID, from.Truncate(time.Hour), to)
	case "day":
		return s.historyRollup("readings_daily", stationID, from.Truncate(24*time.Hour), to)
	default:
		return nil, fmt.Errorf("invalid bucket %q (want raw|hour|day)", bucket)
	}
}

func (s *Store) historyRaw(stationID string, from, to time.Time) ([]HistoryBucket, error) {
	rows, err := s.db.Query(`
		SELECT ts AS bucket, COUNT(*),
		       AVG(temp_c), AVG(humidity_pct), AVG(pressure_hpa), AVG(uv_index),
		       AVG(wind_avg_mps), MAX(wind_gust_mps), SUM(rain_mm), AVG(soil_moisture_pct)
		FROM readings WHERE station_id=? AND ts>=? AND ts<?
		GROUP BY bucket ORDER BY bucket`, stationID, isoUTC(from), isoUTC(to))
	if err != nil {
		return nil, err
	}
	return scanHistory(rows)
}

func (s *Store) historyRollup(table, stationID string, from, to time.Time) ([]HistoryBucket, error) {
	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT bucket, count, temp_avg, humidity_avg, pressure_avg, uv_avg,
		       wind_avg, wind_gust_max, rain_sum, soil_avg
		FROM %s WHERE station_id=? AND bucket>=? AND bucket<? ORDER BY bucket`, table),
		stationID, isoUTC(from), isoUTC(to))
	if err != nil {
		return nil, err
	}
	return scanHistory(rows)
}

// scanHistory reads rows of (bucket, count, temp, humidity, pressure, uv,
// wind_avg, wind_gust, rain, soil) — the column order shared by both queries.
func scanHistory(rows *sql.Rows) ([]HistoryBucket, error) {
	defer rows.Close()
	var out []HistoryBucket
	for rows.Next() {
		var (
			b                                      HistoryBucket
			bucketS                                string
			temp, hum, pres, uv, wavg, wgust, soil sql.NullFloat64
			rain                                   sql.NullFloat64
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

// RollupHourly recomputes hourly rollups for every raw reading at/after `since`.
// Idempotent (INSERT OR REPLACE keyed on station+bucket), so safe to run often
// and it absorbs late-arriving data.
func (s *Store) RollupHourly(since time.Time) error {
	return s.rollup("readings_hourly", "substr(ts,1,13) || ':00:00Z'", since)
}

// RollupDaily recomputes daily rollups for every raw reading at/after `since`.
func (s *Store) RollupDaily(since time.Time) error {
	return s.rollup("readings_daily", "substr(ts,1,10) || 'T00:00:00Z'", since)
}

func (s *Store) rollup(table, bucketExpr string, since time.Time) error {
	_, err := s.db.Exec(fmt.Sprintf(`
		INSERT OR REPLACE INTO %s
			(station_id, bucket, count, temp_avg, temp_min, temp_max, humidity_avg,
			 pressure_avg, pressure_min, pressure_max, uv_avg, uv_max,
			 wind_avg, wind_gust_max, rain_sum, soil_avg)
		SELECT station_id, %s AS bucket, COUNT(*),
		       AVG(temp_c), MIN(temp_c), MAX(temp_c), AVG(humidity_pct),
		       AVG(pressure_hpa), MIN(pressure_hpa), MAX(pressure_hpa), AVG(uv_index), MAX(uv_index),
		       AVG(wind_avg_mps), MAX(wind_gust_mps), SUM(rain_mm), AVG(soil_moisture_pct)
		FROM readings WHERE ts >= ?
		GROUP BY station_id, bucket`, table, bucketExpr), isoUTC(since))
	return err
}

// PruneRaw deletes raw readings older than `before`, returning the row count.
// Call only after rollups covering that range have been computed.
func (s *Store) PruneRaw(before time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM readings WHERE ts < ?`, isoUTC(before))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
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
		sum                                 Summary
		tmin, tmax, tavg, havg              sql.NullFloat64
		pmin, pmax, pavg, wavg, wgust, rain sql.NullFloat64
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

// UpsertForecast replaces the stored forecast for `source` with `pts` (each an
// hourly point, metric/SI). Existing hours are overwritten so a refreshed
// projection supersedes the prior one; new hours are added.
func (s *Store) UpsertForecast(source string, pts []forecast.Point, fetchedAt time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO forecast
			(source, ts, fetched_at, temp_c, humidity_pct, pressure_hpa, precip_mm, precip_prob, cloud_pct, wind_mps, wind_dir_deg, condition)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	fa := isoUTC(fetchedAt)
	for _, p := range pts {
		if _, err := stmt.Exec(source, isoUTC(p.TS), fa,
			fptr(p.TempC), fptr(p.HumidityPct), fptr(p.PressureHpa),
			fptr(p.PrecipMm), fptr(p.PrecipProb), fptr(p.CloudPct),
			fptr(p.WindMps), fptr(p.WindDirDeg), p.Condition); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Forecast returns stored forecast points for `source` in [from,to), ordered by
// time, along with the source actually used. An empty source picks whichever
// source has the most rows (the active one).
func (s *Store) Forecast(source string, from, to time.Time) ([]forecast.Point, string, error) {
	if source == "" {
		_ = s.db.QueryRow(`SELECT source FROM forecast GROUP BY source ORDER BY COUNT(*) DESC LIMIT 1`).Scan(&source)
	}
	rows, err := s.db.Query(`
		SELECT ts, temp_c, humidity_pct, pressure_hpa, precip_mm, precip_prob, cloud_pct, wind_mps, wind_dir_deg, condition
		FROM forecast WHERE source=? AND ts>=? AND ts<? ORDER BY ts`,
		source, isoUTC(from), isoUTC(to))
	if err != nil {
		return nil, source, err
	}
	defer rows.Close()
	var out []forecast.Point
	for rows.Next() {
		var (
			p                                                 forecast.Point
			tsS                                               string
			temp, hum, pres, precip, pprob, cloud, wind, wdir sql.NullFloat64
			cond                                              sql.NullString
		)
		if err := rows.Scan(&tsS, &temp, &hum, &pres, &precip, &pprob, &cloud, &wind, &wdir, &cond); err != nil {
			return nil, source, err
		}
		p.TS, _ = time.Parse(time.RFC3339, tsS)
		p.TempC, p.HumidityPct, p.PressureHpa = nullF(temp), nullF(hum), nullF(pres)
		p.PrecipMm, p.PrecipProb, p.CloudPct = nullF(precip), nullF(pprob), nullF(cloud)
		p.WindMps, p.WindDirDeg = nullF(wind), nullF(wdir)
		p.Condition = cond.String
		out = append(out, p)
	}
	return out, source, rows.Err()
}

// PruneForecast deletes forecast rows older than `before` (past hours that have
// been superseded by real readings), returning the row count.
func (s *Store) PruneForecast(before time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM forecast WHERE ts < ?`, isoUTC(before))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
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
