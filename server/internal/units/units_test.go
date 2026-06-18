package units

import "testing"

func f(v float64) *float64 { return &v }

func deref(p *float64) float64 {
	if p == nil {
		return -999
	}
	return *p
}

func TestParse(t *testing.T) {
	cases := map[string]string{
		"imperial": "imperial", "US": "imperial", "f": "imperial",
		"metric": "metric", "": "metric", "nonsense": "metric",
	}
	for in, want := range cases {
		if got := Parse(in).Name(); got != want {
			t.Errorf("Parse(%q).Name() = %q, want %q", in, got, want)
		}
	}
}

func TestTemp(t *testing.T) {
	if got := Metric.Temp(f(21.4)); got == nil || *got != 21.4 {
		t.Errorf("metric temp = %v", got)
	}
	if got := Imperial.Temp(f(0)); got == nil || *got != 32.0 {
		t.Errorf("0C should be 32F, got %v", got)
	}
	if got := Imperial.Temp(f(100)); got == nil || *got != 212.0 {
		t.Errorf("100C should be 212F, got %v", got)
	}
	if Imperial.Temp(nil) != nil {
		t.Error("nil temp should stay nil")
	}
}

func TestPressure(t *testing.T) {
	// 1013.2 hPa ≈ 29.92 inHg
	got := Imperial.Pressure(f(1013.2))
	if got == nil || *got != 29.92 {
		t.Errorf("pressure imperial = %v, want 29.92", got)
	}
	if got := Metric.Pressure(f(1013.16)); got == nil || *got != 1013.2 {
		t.Errorf("metric pressure rounding = %v, want 1013.2", deref(got))
	}
}

func TestSpeed(t *testing.T) {
	// 10 m/s ≈ 22.4 mph
	if got := Imperial.SpeedV(10); got != 22.4 {
		t.Errorf("10 m/s = %v mph, want 22.4", got)
	}
	if got := Metric.SpeedV(2.456); got != 2.5 {
		t.Errorf("metric speed rounding = %v, want 2.5", got)
	}
	if Imperial.Speed(nil) != nil {
		t.Error("nil speed should stay nil")
	}
}

func TestRain(t *testing.T) {
	// 25.4 mm = 1 inch
	if got := Imperial.RainV(25.4); got != 1.0 {
		t.Errorf("25.4mm = %v in, want 1.0", got)
	}
	if got := Metric.RainV(0.2794); got != 0.28 {
		t.Errorf("metric rain rounding = %v, want 0.28", got)
	}
}

func TestDistance(t *testing.T) {
	// 10 km ≈ 6.2 mi
	if got := Imperial.Distance(10); got != 6.2 {
		t.Errorf("10km = %v mi, want 6.2", got)
	}
	if got := Metric.Distance(10); got != 10 {
		t.Errorf("metric distance = %v, want 10", got)
	}
}

func TestPctAndUV(t *testing.T) {
	// Unit-independent, rounded to 1 dp, nil-safe.
	if got := Metric.Pct(f(55.5666)); got == nil || *got != 55.6 {
		t.Errorf("Pct = %v, want 55.6", deref(got))
	}
	if got := Imperial.UV(f(3.14)); got == nil || *got != 3.1 {
		t.Errorf("UV = %v, want 3.1", deref(got))
	}
	if Metric.Pct(nil) != nil || Imperial.UV(nil) != nil {
		t.Error("nil should stay nil")
	}
}

func TestLabels(t *testing.T) {
	if Metric.Labels().Temp != "°C" || Metric.Labels().Speed != "m/s" {
		t.Errorf("metric labels wrong: %+v", Metric.Labels())
	}
	if Imperial.Labels().Temp != "°F" || Imperial.Labels().Rain != "in" {
		t.Errorf("imperial labels wrong: %+v", Imperial.Labels())
	}
}
