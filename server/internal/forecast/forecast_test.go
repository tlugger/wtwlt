package forecast

import (
	"math"
	"testing"
	"time"
)

func approx(t *testing.T, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("got nil, want %v", want)
	}
	if math.Abs(*got-want) > 0.01 {
		t.Fatalf("got %v, want %v", *got, want)
	}
}

func TestParseOpenMeteo(t *testing.T) {
	body := []byte(`{
	  "hourly": {
	    "time": ["2026-06-25T00:00", "2026-06-25T01:00"],
	    "temperature_2m": [18.5, 17.0],
	    "relative_humidity_2m": [55, 60],
	    "surface_pressure": [840.2, 841.0],
	    "precipitation": [0.0, 1.2],
	    "wind_speed_10m": [3.4, null],
	    "wind_direction_10m": [270, 280]
	  }
	}`)
	pts, err := parseOpenMeteo(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 {
		t.Fatalf("got %d points, want 2", len(pts))
	}
	want := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	if !pts[0].TS.Equal(want) {
		t.Errorf("ts = %v, want %v", pts[0].TS, want)
	}
	approx(t, pts[0].TempC, 18.5)
	approx(t, pts[0].HumidityPct, 55)
	approx(t, pts[0].PressureHpa, 840.2)
	approx(t, pts[0].PrecipMm, 0.0)
	approx(t, pts[0].WindMps, 3.4)
	approx(t, pts[0].WindDirDeg, 270)
	// null wind speed -> nil pointer (graceful-degradation convention)
	if pts[1].WindMps != nil {
		t.Errorf("null wind_speed should parse to nil, got %v", *pts[1].WindMps)
	}
}

func TestParseOpenMeteoEmpty(t *testing.T) {
	if _, err := parseOpenMeteo([]byte(`{"hourly":{"time":[]}}`)); err == nil {
		t.Fatal("expected error on empty series")
	}
}

func TestParseNWSPoints(t *testing.T) {
	url, err := parseNWSPoints([]byte(`{"properties":{"forecastHourly":"https://api.weather.gov/gridpoints/BOU/63,62/forecast/hourly"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://api.weather.gov/gridpoints/BOU/63,62/forecast/hourly" {
		t.Errorf("unexpected url %q", url)
	}
	if _, err := parseNWSPoints([]byte(`{"properties":{}}`)); err == nil {
		t.Fatal("expected error when forecastHourly missing")
	}
}

func TestParseNWSForecast(t *testing.T) {
	// As returned with ?units=si: temperature in °C, wind as "N km/h".
	body := []byte(`{"properties":{"periods":[
	  {"startTime":"2026-06-25T08:00:00-06:00","temperature":18,"temperatureUnit":"C",
	   "relativeHumidity":{"value":50},"windSpeed":"18 km/h","windDirection":"NW"},
	  {"startTime":"2026-06-25T09:00:00-06:00","temperature":20,"temperatureUnit":"C",
	   "relativeHumidity":{"value":45},"windSpeed":"","windDirection":""}
	]}}`)
	pts, err := parseNWSForecast(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 {
		t.Fatalf("got %d points, want 2", len(pts))
	}
	// -06:00 offset normalized to UTC.
	want := time.Date(2026, 6, 25, 14, 0, 0, 0, time.UTC)
	if !pts[0].TS.Equal(want) {
		t.Errorf("ts = %v, want %v", pts[0].TS, want)
	}
	approx(t, pts[0].TempC, 18)
	approx(t, pts[0].HumidityPct, 50)
	approx(t, pts[0].WindMps, 18.0/3.6)
	approx(t, pts[0].WindDirDeg, 315)
	// NWS supplies neither pressure nor a precip amount.
	if pts[0].PressureHpa != nil || pts[0].PrecipMm != nil {
		t.Error("nws should leave pressure and precip nil")
	}
	// blank wind fields -> nil
	if pts[1].WindMps != nil || pts[1].WindDirDeg != nil {
		t.Error("blank wind should parse to nil")
	}
}

func TestParseNWSForecastFahrenheit(t *testing.T) {
	body := []byte(`{"properties":{"periods":[
	  {"startTime":"2026-06-25T08:00:00+00:00","temperature":72,"temperatureUnit":"F",
	   "relativeHumidity":{"value":40},"windSpeed":"10 mph","windDirection":"E"}]}}`)
	pts, err := parseNWSForecast(body)
	if err != nil {
		t.Fatal(err)
	}
	approx(t, pts[0].TempC, 22.222) // 72°F
	approx(t, pts[0].WindMps, 10*0.44704)
	approx(t, pts[0].WindDirDeg, 90)
}

func TestNew(t *testing.T) {
	for _, name := range []string{"openmeteo", "open-meteo", "nws", "noaa"} {
		p, err := New(name, nil)
		if err != nil || p == nil {
			t.Errorf("New(%q) = %v, %v", name, p, err)
		}
	}
	for _, name := range []string{"", "none", "off"} {
		p, err := New(name, nil)
		if err != nil || p != nil {
			t.Errorf("New(%q) should disable: got %v, %v", name, p, err)
		}
	}
	if _, err := New("bogus", nil); err == nil {
		t.Error("New(bogus) should error")
	}
}

func TestCardinalToDeg(t *testing.T) {
	cases := map[string]float64{"n": 0, "ESE": 112.5, "sw": 225, "NNW": 337.5}
	for in, want := range cases {
		approx(t, cardinalToDeg(in), want)
	}
	if cardinalToDeg("XYZ") != nil {
		t.Error("unknown cardinal should be nil")
	}
}
