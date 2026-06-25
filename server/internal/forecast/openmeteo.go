package forecast

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// openMeteoBase is the keyless Open-Meteo forecast endpoint (no API key, no
// signup). https://open-meteo.com/en/docs
const openMeteoBase = "https://api.open-meteo.com/v1/forecast"

// openMeteo is the default provider: a single request returns every quantity we
// track, already in metric/SI (we ask for m/s wind), so it maps 1:1 to Point.
type openMeteo struct {
	hc   *http.Client
	base string
}

func (o *openMeteo) Name() string { return "openmeteo" }

func (o *openMeteo) Fetch(ctx context.Context, lat, lon float64) ([]Point, error) {
	// forecast_days=3 guarantees ≥48 h ahead regardless of the current hour
	// (the day count is calendar days from today). timezone=UTC so the naive
	// timestamps are UTC; wind_speed_unit=ms keeps it SI like everything else.
	url := fmt.Sprintf("%s?latitude=%.4f&longitude=%.4f"+
		"&hourly=temperature_2m,relative_humidity_2m,surface_pressure,precipitation,wind_speed_10m,wind_direction_10m"+
		"&wind_speed_unit=ms&timezone=UTC&forecast_days=3", o.base, lat, lon)
	body, err := getBody(ctx, o.hc, url, defaultUA, "application/json")
	if err != nil {
		return nil, err
	}
	return parseOpenMeteo(body)
}

// omResp mirrors the parallel-arrays shape Open-Meteo returns. Values can be
// null, so each series is []*float64.
type omResp struct {
	Hourly struct {
		Time      []string   `json:"time"`
		Temp      []*float64 `json:"temperature_2m"`
		Humidity  []*float64 `json:"relative_humidity_2m"`
		Pressure  []*float64 `json:"surface_pressure"`
		Precip    []*float64 `json:"precipitation"`
		WindSpeed []*float64 `json:"wind_speed_10m"`
		WindDir   []*float64 `json:"wind_direction_10m"`
	} `json:"hourly"`
}

func parseOpenMeteo(body []byte) ([]Point, error) {
	var r omResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("openmeteo: %w", err)
	}
	h := r.Hourly
	n := len(h.Time)
	if n == 0 {
		return nil, fmt.Errorf("openmeteo: empty hourly series")
	}
	at := func(s []*float64, i int) *float64 {
		if i < len(s) {
			return s[i]
		}
		return nil
	}
	out := make([]Point, 0, n)
	for i, ts := range h.Time {
		// Open-Meteo with timezone=UTC emits naive "2006-01-02T15:04" in UTC.
		t, err := time.Parse("2006-01-02T15:04", ts)
		if err != nil {
			continue
		}
		out = append(out, Point{
			TS:          t.UTC(),
			TempC:       at(h.Temp, i),
			HumidityPct: at(h.Humidity, i),
			PressureHpa: at(h.Pressure, i),
			PrecipMm:    at(h.Precip, i),
			WindMps:     at(h.WindSpeed, i),
			WindDirDeg:  at(h.WindDir, i),
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("openmeteo: no parseable timestamps")
	}
	return out, nil
}
