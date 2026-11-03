package sld

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// icons known to the tests (dev/icons).
func testIcons(href string) (string, bool) {
	base := strings.ToLower(filepath.Base(href))
	switch base {
	case "city.png", "capital.png", "school.png":
		return "icon_" + strings.TrimSuffix(base, ".png"), true
	}
	return "", false
}

// fixture reads dev/sld/<name> from the repo root; tests skip when the
// package is built outside the repo (e.g. msapi-only Docker context).
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "dev", "sld", name))
	if err != nil {
		t.Skipf("fixture %s not available: %v", name, err)
	}
	return b
}

func mustContain(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in:\n%s", w, out)
		}
	}
}

func TestCitiesIcons(t *testing.T) {
	p, err := Parse("sld", fixture(t, "cities_icons.sld"), testIcons)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := p.Render("cities_icons", "POINT")
	mustContain(t, out,
		`NAME "Megacities"`,
		`GROUP "cities_icons"`,
		`EXPRESSION ([population] >= 10000000)`,
		`SYMBOL "icon_capital"`,
		`SIZE 24`,
		`SYMBOL "icon_city"`,
	)
	if strings.Index(out, "Megacities") > strings.Index(out, "Other cities") {
		t.Error("ElseFilter rule must come last")
	}
	if strings.Contains(out, "GEOMTRANSFORM") {
		t.Error("no centroid transform on a point layer")
	}
	if len(p.UsedIcons) != 2 {
		t.Errorf("used icons = %v", p.UsedIcons)
	}
}

func TestCitiesFiltered(t *testing.T) {
	p, err := Parse("sld", fixture(t, "cities_filtered.sld"), testIcons)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := p.Render("cities_filtered", "POINT")
	mustContain(t, out,
		`EXPRESSION ("[state]" = "Maharashtra")`,
		`EXPRESSION ([population] >= 10000000 AND [population] <= 20000000)`,
		`EXPRESSION (("[name]" ~ "^K.*$") OR ("[name]" ~ "^L.*$"))`,
		`SYMBOL "star"`,
		`COLOR "#f97316"`,
		`COLOR "#2563ebcc"`, // fill-opacity 0.8
		`WIDTH 1.5`,
		`TEXT "[name]"`,
		`FONT "sans-bold"`,
		`OUTLINECOLOR "#ffffff"`,
		`OUTLINEWIDTH 2`,
		`POSITION cr`,
		`OFFSET 8 0`,
	)
}

func TestRiversOnLineAndPolygonLayers(t *testing.T) {
	p, err := Parse("sld", fixture(t, "rivers_dashed.sld"), nil)
	if err != nil {
		t.Fatal(err)
	}
	line, _ := p.Render("rivers_dashed", "LINE")
	mustContain(t, line,
		`EXPRESSION ([length_km] > 1400)`,
		`COLOR "#bfdbfe"`,
		`LINECAP ROUND`,
		`LINEJOIN ROUND`,
		`PATTERN 6 3 END`,
		`COLOR "#3b82f6cc"`,
		`MAXSCALEDENOM 20000000`,
	)
	poly, _ := p.Render("rivers_dashed", "POLYGON")
	mustContain(t, poly, `OUTLINECOLOR "#1d4ed8"`)
	if strings.Contains(poly, `  COLOR "#1d4ed8"`) {
		t.Error("on polygons a LineSymbolizer must be an outline")
	}
	_, warns := p.Render("rivers_dashed", "POINT")
	if len(warns) == 0 {
		t.Error("expected warnings for line symbolizers on a point layer")
	}
}

func TestStatesSE11(t *testing.T) {
	p, err := Parse("sld", fixture(t, "states_labelled.sld"), nil)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := p.Render("states_labelled", "POLYGON")
	mustContain(t, out,
		`NAME "East"`,
		`EXPRESSION ("[region]" = "east")`,
		`COLOR "#fde68ab3"`,
		`OUTLINECOLOR "#b45309"`,
		`POSITION cc`,
		`FONT "sans"`,
		`FONT "serif-italic"`,
		`PATTERN 4 2 END`,
	)
	found := false
	for _, w := range p.Warnings {
		if strings.Contains(w, "maxDisplacement") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a VendorOption warning, got %v", p.Warnings)
	}
}

func TestDEMRamp(t *testing.T) {
	p, err := Parse("sld", fixture(t, "dem_ramp.sld"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Target != "raster" || p.Opacity == nil || *p.Opacity != 0.85 {
		t.Fatalf("target=%s opacity=%v", p.Target, p.Opacity)
	}
	out, _ := p.Render("dem_ramp", "RASTER")
	mustContain(t, out,
		`EXPRESSION ([pixel] >= 0 AND [pixel] < 10)`,
		`COLORRANGE 43 131 186 171 221 164`,
		`DATARANGE 0 10`,
		`EXPRESSION ([pixel] >= 35 AND [pixel] <= 60)`,
	)
	if strings.Contains(out, "-9999") {
		t.Error("transparent no-data segment must be skipped")
	}
}

func TestJSONStyle(t *testing.T) {
	p, err := Parse("json", fixture(t, "cities_simple.json"), testIcons)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := p.Render("cities_simple", "POINT")
	mustContain(t, out,
		`EXPRESSION ([population] >= 10000000)`,
		`SYMBOL "icon_capital"`,
		`TEXT "[name]"`,
		`EXPRESSION ("[state]" = "West Bengal")`,
		`SYMBOL "star"`,
	)
}

func TestMissingIcon(t *testing.T) {
	sld := `<StyledLayerDescriptor><NamedLayer><UserStyle><FeatureTypeStyle><Rule>
	  <PointSymbolizer><Graphic><ExternalGraphic>
	    <OnlineResource xmlns:xlink="http://www.w3.org/1999/xlink" xlink:href="icons/nope.png"/>
	  </ExternalGraphic></Graphic></PointSymbolizer>
	</Rule></FeatureTypeStyle></UserStyle></NamedLayer></StyledLayerDescriptor>`
	_, err := Parse("sld", []byte(sld), testIcons)
	var m *MissingIconsError
	if !errors.As(err, &m) || len(m.Names) != 1 || m.Names[0] != "icons/nope.png" {
		t.Fatalf("err = %v", err)
	}
}

func TestRendersNothing(t *testing.T) {
	sld := `<StyledLayerDescriptor><NamedLayer><UserStyle><FeatureTypeStyle><Rule>
	  <ogc:Filter xmlns:ogc="http://www.opengis.net/ogc"><ogc:BBOX/></ogc:Filter>
	  <LineSymbolizer/>
	</Rule></FeatureTypeStyle></UserStyle></NamedLayer></StyledLayerDescriptor>`
	_, err := Parse("sld", []byte(sld), nil)
	if !errors.Is(err, ErrRendersNothing) {
		t.Fatalf("err = %v", err)
	}
}

func TestCQL(t *testing.T) {
	cases := map[string]string{
		`type = 'school'`:             `("[type]" = "school")`,
		`pop >= 1000 AND type <> 'x'`: `(([pop] >= 1000) AND ("[type]" != "x"))`,
		`NOT (a = 1 OR b = 2)`:        `(NOT (([a] = 1) OR ([b] = 2)))`,
		`h BETWEEN 10 AND 20`:         `([h] >= 10 AND [h] <= 20)`,
		`name LIKE 'Ko%'`:             `("[name]" ~ "^Ko.*$")`,
		`code IS NOT NULL`:            `("[code]" != "")`,
		`label = 'it''s'`:             `("[label]" = "it's")`,
	}
	for in, want := range cases {
		got, err := cqlToExpr(in)
		if err != nil || got != want {
			t.Errorf("%s → %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{`a =`, `a IN (1,2)`, `(a = 1`, `'x' = a`} {
		if _, err := cqlToExpr(bad); err == nil {
			t.Errorf("%s should fail", bad)
		}
	}
}

func TestPosition(t *testing.T) {
	for _, c := range []struct {
		x, y float64
		want string
	}{{0, 0.5, "cr"}, {0.5, 0.5, "cc"}, {1, 0.5, "cl"}, {0.5, 0, "uc"}, {0.5, 1, "lc"}} {
		if got := position(c.x, c.y); got != c.want {
			t.Errorf("position(%v,%v) = %s, want %s", c.x, c.y, got, c.want)
		}
	}
}
