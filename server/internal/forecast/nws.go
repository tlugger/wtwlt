package forecast

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// nwsBase is the keyless US National Weather Service API (no API key; a
// User-Agent header is required). https://www.weather.gov/documentation/services-web-api
const nwsBase = "https://api.weather.gov"

// nws is the official-US-source provider. Its hourly forecast is metric-by-
// request (?units=si) but is weaker than Open-Meteo: it carries no barometric
// pressure and only a precip *probability* (not an accumulation in mm), so both
// PressureHpa and PrecipMm are left nil. This exercises the nil-field path.
type nws struct {
	hc   *http.Client
	base string
	ua   string
}

func (n *nws) Name() string { return "nws" }

func (n *nws) Fetch(ctx context.Context, lat, lon float64) ([]Point, error) {
	// Hop 1: resolve the gridpoint -> the hourly-forecast URL for this location.
	ptURL := fmt.Sprintf("%s/points/%.4f,%.4f", n.base, lat, lon)
	ptBody, err := getBody(ctx, n.hc, ptURL, n.ua, "application/geo+json")
	if err != nil {
		return nil, err
	}
	fcURL, err := parseNWSPoints(ptBody)
	if err != nil {
		return nil, err
	}
	// Hop 2: the hourly forecast, requested in SI units (°C, km/h).
	sep := "?"
	if strings.Contains(fcURL, "?") {
		sep = "&"
	}
	fcBody, err := getBody(ctx, n.hc, fcURL+sep+"units=si", n.ua, "application/geo+json")
	if err != nil {
		return nil, err
	}
	return parseNWSForecast(fcBody)
}

type nwsPoints struct {
	Properties struct {
		ForecastHourly string `json:"forecastHourly"`
	} `json:"properties"`
}

func parseNWSPoints(body []byte) (string, error) {
	var p nwsPoints
	if err := json.Unmarshal(body, &p); err != nil {
		return "", fmt.Errorf("nws points: %w", err)
	}
	if p.Properties.ForecastHourly == "" {
		return "", fmt.Errorf("nws points: no forecastHourly URL")
	}
	return p.Properties.ForecastHourly, nil
}

type nwsForecast struct {
	Properties struct {
		Periods []struct {
			StartTime        string  `json:"startTime"`
			Temperature      float64 `json:"temperature"`
			TemperatureUnit  string  `json:"temperatureUnit"`
			RelativeHumidity struct {
				Value *float64 `json:"value"`
			} `json:"relativeHumidity"`
			ProbabilityOfPrecipitation struct {
				Value *float64 `json:"value"`
			} `json:"probabilityOfPrecipitation"`
			WindSpeed     string `json:"windSpeed"`
			WindDirection string `json:"windDirection"`
			ShortForecast string `json:"shortForecast"`
		} `json:"periods"`
	} `json:"properties"`
}

// shortForecastCondition maps NWS free-text ("Mostly Sunny", "Chance Showers
// And Thunderstorms") to our vocabulary by keyword, most-significant first.
func shortForecastCondition(s string) string {
	l := strings.ToLower(s)
	switch {
	case strings.Contains(l, "thunder"):
		return CondThunder
	case strings.Contains(l, "snow"), strings.Contains(l, "flurr"), strings.Contains(l, "sleet"), strings.Contains(l, "blizzard"):
		return CondSnow
	case strings.Contains(l, "drizzle"):
		return CondDrizzle
	case strings.Contains(l, "rain"), strings.Contains(l, "shower"):
		return CondRain
	case strings.Contains(l, "fog"), strings.Contains(l, "haze"):
		return CondFog
	case strings.Contains(l, "partly"), strings.Contains(l, "mostly sunny"):
		return CondPartly
	case strings.Contains(l, "cloud"), strings.Contains(l, "overcast"):
		return CondCloudy
	case strings.Contains(l, "sunny"), strings.Contains(l, "clear"), strings.Contains(l, "fair"):
		return CondClear
	default:
		return ""
	}
}

func parseNWSForecast(body []byte) ([]Point, error) {
	var f nwsForecast
	if err := json.Unmarshal(body, &f); err != nil {
		return nil, fmt.Errorf("nws forecast: %w", err)
	}
	periods := f.Properties.Periods
	if len(periods) == 0 {
		return nil, fmt.Errorf("nws forecast: no periods")
	}
	out := make([]Point, 0, len(periods))
	for _, p := range periods {
		t, err := time.Parse(time.RFC3339, p.StartTime)
		if err != nil {
			continue
		}
		tempC := p.Temperature
		if strings.EqualFold(p.TemperatureUnit, "F") {
			tempC = (tempC - 32) * 5 / 9
		}
		out = append(out, Point{
			TS:          t.UTC(),
			TempC:       ptr(tempC),
			HumidityPct: p.RelativeHumidity.Value,
			PrecipProb:  p.ProbabilityOfPrecipitation.Value,
			WindMps:     parseWindSpeed(p.WindSpeed),
			WindDirDeg:  cardinalToDeg(p.WindDirection),
			Condition:   shortForecastCondition(p.ShortForecast),
			// PressureHpa, PrecipMm, CloudPct: not in the NWS hourly product.
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("nws forecast: no parseable periods")
	}
	return out, nil
}

// parseWindSpeed turns NWS's "10 km/h" / "5 mph" string into m/s (nil if blank).
func parseWindSpeed(s string) *float64 {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return nil
	}
	switch {
	case strings.Contains(s, "km/h"), strings.Contains(s, "kph"):
		v /= 3.6
	case strings.Contains(s, "mph"):
		v *= 0.44704
	}
	return ptr(v)
}

// cardinalToDeg maps a 16-point compass abbreviation to degrees (nil if blank).
func cardinalToDeg(c string) *float64 {
	deg, ok := map[string]float64{
		"N": 0, "NNE": 22.5, "NE": 45, "ENE": 67.5,
		"E": 90, "ESE": 112.5, "SE": 135, "SSE": 157.5,
		"S": 180, "SSW": 202.5, "SW": 225, "WSW": 247.5,
		"W": 270, "WNW": 292.5, "NW": 315, "NNW": 337.5,
	}[strings.ToUpper(strings.TrimSpace(c))]
	if !ok {
		return nil
	}
	return ptr(deg)
}
