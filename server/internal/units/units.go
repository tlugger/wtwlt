// Package units converts stored metric/SI values to the requested display system.
// Data is always stored metric; conversion happens at the API layer.
package units

import (
	"math"
	"strings"
)

// System is a display unit system. The zero value is metric.
type System struct{ imperial bool }

var (
	Metric   = System{imperial: false}
	Imperial = System{imperial: true}
)

// Parse maps a query string to a System (default metric).
func Parse(s string) System {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "imperial", "us", "f":
		return Imperial
	default:
		return Metric
	}
}

// Name returns "metric" or "imperial".
func (s System) Name() string {
	if s.imperial {
		return "imperial"
	}
	return "metric"
}

func round(v float64, dp int) float64 {
	p := math.Pow(10, float64(dp))
	return math.Round(v*p) / p
}

func ptr(v float64) *float64 { return &v }

// Temp converts °C (nil-safe). Imperial → °F. Rounded to 1 dp.
func (s System) Temp(c *float64) *float64 {
	if c == nil {
		return nil
	}
	v := *c
	if s.imperial {
		v = v*9.0/5.0 + 32.0
	}
	return ptr(round(v, 1))
}

// Pressure converts hPa (nil-safe). Imperial → inHg (2 dp); metric 1 dp.
func (s System) Pressure(hpa *float64) *float64 {
	if hpa == nil {
		return nil
	}
	if s.imperial {
		return ptr(round(*hpa*0.02952998057228486, 2))
	}
	return ptr(round(*hpa, 1))
}

// Speed converts m/s (nil-safe). Imperial → mph. Rounded to 1 dp.
func (s System) Speed(mps *float64) *float64 {
	if mps == nil {
		return nil
	}
	return ptr(s.SpeedV(*mps))
}

// SpeedV converts a non-nullable m/s value.
func (s System) SpeedV(mps float64) float64 {
	if s.imperial {
		mps *= 2.236936292054402
	}
	return round(mps, 1)
}

// Rain converts mm (nil-safe). Imperial → inches (3 dp); metric 2 dp.
func (s System) Rain(mm *float64) *float64 {
	if mm == nil {
		return nil
	}
	return ptr(s.RainV(*mm))
}

// RainV converts a non-nullable mm value.
func (s System) RainV(mm float64) float64 {
	if s.imperial {
		return round(mm*0.03937007874015748, 3)
	}
	return round(mm, 2)
}

// Pct rounds a percentage (humidity/soil) to 1 dp; unit-independent (nil-safe).
func (s System) Pct(p *float64) *float64 {
	if p == nil {
		return nil
	}
	return ptr(round(*p, 1))
}

// UV rounds the UV index to 1 dp; unit-independent (nil-safe).
func (s System) UV(p *float64) *float64 {
	if p == nil {
		return nil
	}
	return ptr(round(*p, 1))
}

// Distance converts km (as stored in lightning events). Imperial → miles (1 dp).
func (s System) Distance(km int) float64 {
	if s.imperial {
		return round(float64(km)*0.621371192, 1)
	}
	return float64(km)
}

// Labels are the unit symbols for each quantity in this system.
type Labels struct {
	Temp     string `json:"temp"`
	Humidity string `json:"humidity"`
	Pressure string `json:"pressure"`
	UV       string `json:"uv"`
	Speed    string `json:"speed"`
	Rain     string `json:"rain"`
	Soil     string `json:"soil"`
	Distance string `json:"distance"`
}

// Labels returns the unit symbols for this system.
func (s System) Labels() Labels {
	if s.imperial {
		return Labels{Temp: "°F", Humidity: "%", Pressure: "inHg", UV: "index",
			Speed: "mph", Rain: "in", Soil: "%", Distance: "mi"}
	}
	return Labels{Temp: "°C", Humidity: "%", Pressure: "hPa", UV: "index",
		Speed: "m/s", Rain: "mm", Soil: "%", Distance: "km"}
}
