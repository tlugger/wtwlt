package geocode

import "testing"

func TestParseNominatim(t *testing.T) {
	body := []byte(`{"address":{"town":"Thornton","county":"Adams County","state":"Colorado","country_code":"us"}}`)
	got, err := parseNominatim(body)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Thornton, Colorado" {
		t.Errorf("got %q, want %q", got, "Thornton, Colorado")
	}
}

func TestParseNominatimFallsBackToCounty(t *testing.T) {
	// No city/town/village → fall back to county, still with state.
	body := []byte(`{"address":{"county":"Adams County","state":"Colorado"}}`)
	got, err := parseNominatim(body)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Adams County, Colorado" {
		t.Errorf("got %q, want %q", got, "Adams County, Colorado")
	}
}

func TestParseNominatimPrefersCity(t *testing.T) {
	body := []byte(`{"address":{"city":"Denver","county":"Denver County","state":"Colorado"}}`)
	got, _ := parseNominatim(body)
	if got != "Denver, Colorado" {
		t.Errorf("got %q, want %q", got, "Denver, Colorado")
	}
}

func TestParseNominatimNoPlace(t *testing.T) {
	if _, err := parseNominatim([]byte(`{"address":{"country":"United States"}}`)); err == nil {
		t.Error("expected error when no place fields present")
	}
}
