package sld

import (
	"strings"
	"testing"
)

func renderJSON(t *testing.T, style, layerType string) string {
	t.Helper()
	p, err := Parse("json", []byte(style), testIcons)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := p.Render("s", layerType)
	return out
}

func TestJSONLineOptions(t *testing.T) {
	out := renderJSON(t, `{"target":"vector","rules":[{"symbol":"line","stroke":"#ff0000","stroke_width":3,
		"dash":[6,2,1,2],"line_cap":"round","line_join":"bevel","offset":4,
		"casing_color":"#000000","casing_width":1.5,
		"marker":"arrow","marker_size":10,"marker_spacing":60}]}`, "LINE")
	mustContain(t, out,
		`COLOR "#000000"`, `WIDTH 6`, // casing: 3 + 2*1.5
		`COLOR "#ff0000"`, `WIDTH 3`,
		`PATTERN 6 2 1 2 END`, `LINECAP ROUND`, `LINEJOIN BEVEL`, `OFFSET 4 -99`,
		`SYMBOL "arrow"`, `SIZE 10`, `GAP -60`,
	)
	if strings.Index(out, `"#000000"`) > strings.Index(out, `"#ff0000"`) {
		t.Error("casing must be drawn before (under) the line")
	}
	if strings.Count(out, "PATTERN") != 1 {
		t.Error("casing must not be dashed")
	}
}

func TestJSONPointOptions(t *testing.T) {
	out := renderJSON(t, `{"target":"vector","rules":[{"mark":"diamond","fill":"#00ff00","size_attr":"pop_size",
		"rotation_attr":"heading","offset_x":2,"offset_y":-3,
		"label":"name","font_style":"italic","label_position":"n","label_offset_y":-4,"label_allow_overlap":true,
		"label_priority":7}]}`, "POINT")
	mustContain(t, out,
		`SYMBOL "diamond"`, `SIZE [pop_size]`, `ANGLE [heading]`, `OFFSET 2 -3`,
		`FONT "sans-italic"`, `POSITION uc`, `OFFSET 0 -4`, `FORCE TRUE`, `PRIORITY 7`,
	)
}

func TestJSONPolygonPatterns(t *testing.T) {
	out := renderJSON(t, `{"target":"vector","rules":[
		{"name":"h","filter":"kind = 'a'","fill":"#0000ff","fill_pattern":"cross","pattern_spacing":10,"pattern_angle":30,"pattern_width":2,"pattern_background":"#ffffff"},
		{"name":"d","else":true,"fill":"#ff0000","fill_pattern":"dots","pattern_width":4,"pattern_spacing":6,"marker":"triangle"}]}`, "POLYGON")
	mustContain(t, out,
		`SYMBOL "hatch"`, `ANGLE 30`, `ANGLE 120`, `SIZE 10`, `WIDTH 2`, `COLOR "#ffffff"`,
		`SYMBOL "circle"`, `SIZE 4`, `GAP 10`,
		`OUTLINECOLOR "#ff0000"`, // marker on a polygon follows the outline
	)
}

func TestJSONRejectsBadOptions(t *testing.T) {
	for name, style := range map[string]string{
		"cap":       `{"rules":[{"stroke":"#000000","line_cap":"pointy"}]}`,
		"pattern":   `{"rules":[{"fill":"#000000","fill_pattern":"zigzag"}]}`,
		"size_attr": `{"rules":[{"mark":"circle","size_attr":"a b"}]}`,
		"marker":    `{"rules":[{"stroke":"#000000","marker":"blob"}]}`,
		"position":  `{"rules":[{"mark":"circle","label":"n","label_position":"up"}]}`,
		"priority":  `{"rules":[{"mark":"circle","label":"n","label_priority":11}]}`,
		"dash":      `{"rules":[{"stroke":"#000000","dash":[4,-1]}]}`,
	} {
		if _, err := Parse("json", []byte(style), testIcons); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
