package inspect

import (
	"math"
	"testing"
)

func TestAngleDeg(t *testing.T) {
	f := map[string]float64{"EPSG:9102": math.Pi / 180}
	for _, c := range []struct {
		v    float64
		uom  string
		want float64
	}{
		{80, "EPSG:9102", 80},
		{23.3, "EPSG:9110", 23.5},        // 23°30'
		{-12.2830, "EPSG:9110", -12.475}, // -12°28'30"
	} {
		if got := angleDeg(c.v, c.uom, f); math.Abs(got-c.want) > 1e-6 {
			t.Errorf("angleDeg(%v, %s) = %v, want %v", c.v, c.uom, got, c.want)
		}
	}
}

func TestRoughBox(t *testing.T) {
	// WGS 84 / India NSF LCC (EPSG:7755): origin 80°E 24°N at (4000000, 4000000).
	c := crsInfo{west: 65.6, south: 3.87, east: 97.42, north: 35.51, lon0: 80, lat0: 24, fe: 4e6, fn: 4e6, unit: 1}
	minx, miny, maxx, maxy := c.roughBox()
	// India's south-west corner in that CRS is about (2.82e6, 2.18e6).
	if !(minx < 2.82e6 && miny < 2.18e6 && maxx > 5.5e6 && maxy > 5.2e6) {
		t.Errorf("rough box %v %v %v %v does not contain India's corners", minx, miny, maxx, maxy)
	}
}
