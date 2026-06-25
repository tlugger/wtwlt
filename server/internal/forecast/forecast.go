// Package forecast fetches a near-term weather forecast from a pluggable,
// keyless provider and normalizes it to the project's metric/SI conventions
// (°C, %, hPa, mm, m/s, degrees). Forecast data is stored separately from
// sensor readings — it is not measured — and is overlaid on the dashboard as a
// dashed projection alongside the real history.
package forecast

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxBody caps a forecast response read (NWS hourly is ~150 KB).
const maxBody = 4 << 20

// Point is one forecast hour in metric/SI units (the storage + wire
// convention). A nil field means the provider does not supply that quantity
// (e.g. NWS gives no barometric pressure), which is stored as SQL NULL.
type Point struct {
	TS          time.Time
	TempC       *float64
	HumidityPct *float64
	PressureHpa *float64
	PrecipMm    *float64
	PrecipProb  *float64 // probability of precipitation, %
	CloudPct    *float64 // total cloud cover, %
	WindMps     *float64
	WindDirDeg  *float64
	Condition   string // normalized sky/precip condition (see Condition* below)
}

// Normalized condition vocabulary — provider-agnostic so the dashboard maps a
// single small set to icons + labels. "" means unknown.
const (
	CondClear   = "clear"
	CondPartly  = "partly"
	CondCloudy  = "cloudy"
	CondFog     = "fog"
	CondDrizzle = "drizzle"
	CondRain    = "rain"
	CondSnow    = "snow"
	CondThunder = "thunder"
)

// Provider fetches an hourly forecast for a location.
type Provider interface {
	// Name identifies the provider; it is also stored as each row's `source`.
	Name() string
	// Fetch returns hourly forecast points for the coordinates, ordered by time.
	Fetch(ctx context.Context, lat, lon float64) ([]Point, error)
}

// defaultUA identifies this client; NWS rejects requests without a User-Agent.
const defaultUA = "wtwlt-weather-station (github.com/tlugger/wtwlt)"

// New returns the provider named by `name` ("openmeteo" | "nws"), or (nil, nil)
// for "none"/"" — forecasts disabled. hc may be nil (a default client is used).
func New(name string, hc *http.Client) (Provider, error) {
	if hc == nil {
		// Generous timeouts: a Pi's uplink can be slow to bring up TLS just after
		// boot (default TLSHandshakeTimeout is only 10s), and the 7-day, multi-
		// field query can be slow to come back. The poller retries on failure.
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSHandshakeTimeout = 30 * time.Second
		tr.ResponseHeaderTimeout = 45 * time.Second // server time to build the response
		hc = &http.Client{Timeout: 60 * time.Second, Transport: tr}
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "none", "off":
		return nil, nil
	case "openmeteo", "open-meteo":
		return &openMeteo{hc: hc, base: openMeteoBase}, nil
	case "nws", "noaa":
		return &nws{hc: hc, base: nwsBase, ua: defaultUA}, nil
	default:
		return nil, fmt.Errorf("unknown forecast provider %q (want openmeteo|nws|none)", name)
	}
}

// getBody performs a GET with the project User-Agent and returns the body. The
// parse step is kept separate (pure functions over []byte) so it is unit-tested
// without a network round-trip.
func getBody(ctx context.Context, hc *http.Client, url, ua, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBody))
}

func ptr(v float64) *float64 { return &v }
