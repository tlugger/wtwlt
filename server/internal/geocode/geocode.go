// Package geocode resolves a coarse place label (e.g. "Thornton, Colorado")
// from coordinates via the keyless OpenStreetMap Nominatim service. The label
// lets the dashboard show an approximate location without ever exposing — or
// sending to the browser — the station's exact lat/lon.
package geocode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	nominatimBase = "https://nominatim.openstreetmap.org/reverse"
	userAgent     = "wtwlt-weather-station (github.com/tlugger/wtwlt)"
	maxBody       = 1 << 20
)

// Reverse returns a coarse "<place>, <state>" label for the coordinates.
// zoom=10 keeps the result at city/town granularity (no street/house detail).
func Reverse(ctx context.Context, lat, lon float64) (string, error) {
	url := fmt.Sprintf("%s?format=jsonv2&zoom=10&addressdetails=1&lat=%.4f&lon=%.4f", nominatimBase, lat, lon)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent) // Nominatim requires an identifying UA
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("geocode: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return "", err
	}
	return parseNominatim(body)
}

type nomResp struct {
	Address struct {
		City         string `json:"city"`
		Town         string `json:"town"`
		Village      string `json:"village"`
		Municipality string `json:"municipality"`
		County       string `json:"county"`
		State        string `json:"state"`
	} `json:"address"`
}

func parseNominatim(body []byte) (string, error) {
	var r nomResp
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("geocode: %w", err)
	}
	a := r.Address
	// Coarsest-first among the city-level fields; never street/house detail.
	place := firstNonEmpty(a.City, a.Town, a.Village, a.Municipality, a.County)
	if place == "" {
		return "", fmt.Errorf("geocode: no place in response")
	}
	if a.State != "" {
		return place + ", " + a.State, nil
	}
	return place, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
