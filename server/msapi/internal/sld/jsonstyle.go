package sld

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// JSON style shortcut (PLAN.md §6.5):
//
//	{ "target": "vector", "rules": [
//	  { "name": "Schools", "filter": "type = 'school'", "icon": "school.png", "size": 24 },
//	  { "name": "Other", "else": true, "mark": "circle", "fill": "#3366cc", "size": 8 } ]}
//
//	{ "target": "raster", "opacity": 0.8, "colormap": { "type": "ramp",
//	  "entries": [ { "quantity": 0, "color": "#2b83ba" }, { "quantity": 60, "color": "#d7191c" } ] } }
type jsonStyle struct {
	Target   string     `json:"target"`
	Rules    []jsonRule `json:"rules"`
	ColorMap *jsonCMap  `json:"colormap"`
	Opacity  *float64   `json:"opacity"`
}

type jsonRule struct {
	Name     string   `json:"name"`
	Filter   string   `json:"filter"`
	Else     bool     `json:"else"`
	MinScale *float64 `json:"min_scale"`
	MaxScale *float64 `json:"max_scale"`

	// "point" | "line" | "polygon"; inferred when empty.
	Symbol string `json:"symbol"`

	Icon         string   `json:"icon"`
	Mark         string   `json:"mark"`
	Size         *float64 `json:"size"`
	SizeAttr     string   `json:"size_attr"` // size read from a numeric column
	Rotation     *float64 `json:"rotation"`
	RotationAttr string   `json:"rotation_attr"`
	Opacity      *float64 `json:"opacity"`
	OffsetX      *float64 `json:"offset_x"`
	OffsetY      *float64 `json:"offset_y"`

	Fill          string    `json:"fill"`
	FillOpacity   *float64  `json:"fill_opacity"`
	Stroke        string    `json:"stroke"`
	StrokeWidth   *float64  `json:"stroke_width"`
	StrokeOpacity *float64  `json:"stroke_opacity"`
	Dash          []float64 `json:"dash"`
	LineCap       string    `json:"line_cap"`  // butt | round | square
	LineJoin      string    `json:"line_join"` // round | miter | bevel
	Offset        *float64  `json:"offset"`    // perpendicular line offset, px

	// Casing: a wider line drawn under the stroke (road-style outline).
	CasingColor string   `json:"casing_color"`
	CasingWidth *float64 `json:"casing_width"`

	// Marks repeated along lines / polygon outlines, e.g. direction arrows.
	Marker        string   `json:"marker"`
	MarkerSize    *float64 `json:"marker_size"`
	MarkerSpacing *float64 `json:"marker_spacing"`
	MarkerColor   string   `json:"marker_color"`

	// Polygon fill pattern: solid (default) | hatch | cross | dots.
	FillPattern       string   `json:"fill_pattern"`
	PatternSpacing    *float64 `json:"pattern_spacing"`
	PatternAngle      *float64 `json:"pattern_angle"`
	PatternWidth      *float64 `json:"pattern_width"`
	PatternBackground string   `json:"pattern_background"`

	Label             string   `json:"label"` // attribute name, or text with [attr] bindings
	FontFamily        string   `json:"font_family"`
	FontSize          *float64 `json:"font_size"`
	FontWeight        string   `json:"font_weight"`
	FontStyle         string   `json:"font_style"` // normal | italic
	LabelColor        string   `json:"label_color"`
	HaloColor         string   `json:"halo_color"`
	HaloRadius        *float64 `json:"halo_radius"`
	LabelPosition     string   `json:"label_position"` // auto | center | n | ne | e | se | s | sw | w | nw
	LabelOffsetX      *float64 `json:"label_offset_x"`
	LabelOffsetY      *float64 `json:"label_offset_y"` // px, down
	LabelAngle        *float64 `json:"label_angle"`
	LabelFollowLine   bool     `json:"label_follow_line"`
	LabelAllowOverlap bool     `json:"label_allow_overlap"`
	LabelRepeat       *float64 `json:"label_repeat"` // px between repeats along lines
	LabelPriority     *int     `json:"label_priority"`
}

// labelPositions maps compass positions to MapServer POSITION codes
// (where the label sits relative to the point).
var labelPositions = map[string]string{
	"auto": "auto", "center": "cc", "n": "uc", "ne": "ur", "e": "cr", "se": "lr",
	"s": "lc", "sw": "ll", "w": "cl", "nw": "ul",
}

func oneOf(label, field, v string, allowed ...string) error {
	if v == "" {
		return nil
	}
	for _, a := range allowed {
		if strings.EqualFold(v, a) {
			return nil
		}
	}
	return fmt.Errorf("rule %s: %s must be one of %s", label, field, strings.Join(allowed, ", "))
}

// attrRef turns a column name into a mapfile attribute binding, "[col]".
func attrRef(label, field, v string) (string, error) {
	if !attrRe.MatchString(v) {
		return "", fmt.Errorf("rule %s: %s must be a column name", label, field)
	}
	return "[" + v + "]", nil
}

type jsonCMap struct {
	Type    string `json:"type"`
	Entries []struct {
		Quantity float64  `json:"quantity"`
		Color    string   `json:"color"`
		Opacity  *float64 `json:"opacity"`
		Label    string   `json:"label"`
	} `json:"entries"`
}

func parseJSONStyle(data []byte, icons IconResolver) (*Style, *parser, error) {
	var js jsonStyle
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&js); err != nil {
		return nil, nil, fmt.Errorf("invalid JSON style: %w", err)
	}
	p := &parser{icons: icons, usedIcons: map[string]bool{}}

	switch js.Target {
	case "raster":
		st := &Style{Target: "raster", Opacity: js.Opacity}
		rs := RasterSym{}
		if js.ColorMap != nil {
			rs.MapType = strings.ToLower(js.ColorMap.Type)
			if rs.MapType == "" {
				rs.MapType = "ramp"
			}
			if rs.MapType != "ramp" && rs.MapType != "intervals" && rs.MapType != "values" {
				return nil, p, fmt.Errorf("colormap.type must be ramp, intervals or values")
			}
			for _, e := range js.ColorMap.Entries {
				c, ok := normColor(e.Color)
				if !ok {
					return nil, p, fmt.Errorf("colormap colour %q is not #rrggbb", e.Color)
				}
				rs.Entries = append(rs.Entries, ColorEntry{Quantity: e.Quantity, Color: c, Opacity: e.Opacity, Label: e.Label})
			}
		}
		st.Rules = []Rule{{Name: "raster", Symbols: []Symbolizer{rs}}}
		return st, p, nil
	case "vector", "":
	default:
		return nil, p, fmt.Errorf(`target must be "vector" or "raster"`)
	}

	st := &Style{Target: "vector"}
	for i, jr := range js.Rules {
		label := jr.Name
		if label == "" {
			label = "#" + strconv.Itoa(i+1)
		}
		r := Rule{Name: jr.Name, Else: jr.Else, MinScale: jr.MinScale, MaxScale: jr.MaxScale}
		if jr.Filter != "" {
			if jr.Else {
				return nil, p, fmt.Errorf("rule %s: use either filter or else", label)
			}
			e, err := cqlToExpr(jr.Filter)
			if err != nil {
				return nil, p, fmt.Errorf("rule %s: %w", label, err)
			}
			r.Expr = e
		}

		color := func(v, def string) (string, error) {
			if v == "" {
				return def, nil
			}
			c, ok := normColor(v)
			if !ok {
				return "", fmt.Errorf("rule %s: colour %q is not #rrggbb", label, v)
			}
			return c, nil
		}
		fill, err := color(jr.Fill, "")
		if err != nil {
			return nil, p, err
		}
		stroke, err := color(jr.Stroke, "")
		if err != nil {
			return nil, p, err
		}

		for _, c := range []struct {
			field, v string
			allowed  []string
		}{
			{"line_cap", jr.LineCap, []string{"butt", "round", "square"}},
			{"line_join", jr.LineJoin, []string{"round", "miter", "bevel"}},
			{"fill_pattern", jr.FillPattern, []string{"solid", "hatch", "cross", "dots"}},
			{"font_style", jr.FontStyle, []string{"normal", "italic"}},
		} {
			if err := oneOf(label, c.field, c.v, c.allowed...); err != nil {
				return nil, p, err
			}
		}
		for _, d := range jr.Dash {
			if d < 0 {
				return nil, p, fmt.Errorf("rule %s: dash lengths must not be negative", label)
			}
		}

		kind := jr.Symbol
		if kind == "" {
			switch {
			case jr.Icon != "" || jr.Mark != "":
				kind = "point"
			case fill != "":
				kind = "polygon"
			case stroke != "" || jr.Marker != "":
				kind = "line"
			}
		}
		var strokeSym *Stroke
		if stroke != "" {
			strokeSym = &Stroke{Color: stroke, Width: 1, Opacity: jr.StrokeOpacity, Dash: jr.Dash,
				LineCap: strings.ToLower(jr.LineCap), LineJoin: strings.ToLower(jr.LineJoin)}
			if jr.StrokeWidth != nil {
				strokeSym.Width = *jr.StrokeWidth
			}
			if jr.Offset != nil {
				strokeSym.Offset = *jr.Offset
			}
		}
		var fillSym *Fill
		if fill != "" {
			fillSym = &Fill{Color: fill, Opacity: jr.FillOpacity}
		}
		var markerSym *MarkerLineSym
		if jr.Marker != "" {
			m := strings.ToLower(jr.Marker)
			if _, ok := marks[m]; !ok {
				return nil, p, fmt.Errorf("rule %s: marker %q not supported", label, jr.Marker)
			}
			mc, err := color(jr.MarkerColor, stroke)
			if err != nil {
				return nil, p, err
			}
			if mc == "" {
				mc = fill
			}
			if mc == "" {
				mc = "#3366cc"
			}
			markerSym = &MarkerLineSym{Mark: m, Size: 8, Gap: 40, Color: mc, Opacity: jr.StrokeOpacity}
			if jr.MarkerSize != nil {
				markerSym.Size = *jr.MarkerSize
			}
			if jr.MarkerSpacing != nil {
				markerSym.Gap = *jr.MarkerSpacing
			}
		}

		switch kind {
		case "point":
			ps := PointSym{Opacity: jr.Opacity, Fill: fillSym, Stroke: strokeSym}
			if jr.Size != nil {
				ps.Size = strconv.FormatFloat(*jr.Size, 'f', -1, 64)
			}
			if jr.SizeAttr != "" {
				if ps.Size, err = attrRef(label, "size_attr", jr.SizeAttr); err != nil {
					return nil, p, err
				}
			}
			if jr.Rotation != nil {
				ps.Rotation = strconv.FormatFloat(*jr.Rotation, 'f', -1, 64)
			}
			if jr.RotationAttr != "" {
				if ps.Rotation, err = attrRef(label, "rotation_attr", jr.RotationAttr); err != nil {
					return nil, p, err
				}
			}
			if jr.OffsetX != nil {
				ps.OffsetX = *jr.OffsetX
			}
			if jr.OffsetY != nil {
				ps.OffsetY = *jr.OffsetY
			}
			if jr.Icon != "" {
				sym, ok := resolve(icons, jr.Icon)
				if !ok {
					p.missingIcons = append(p.missingIcons, jr.Icon)
					continue
				}
				p.usedIcons[sym] = true
				ps.Icon = sym
			} else {
				m := strings.ToLower(jr.Mark)
				if m == "" {
					m = "circle"
				}
				if _, ok := marks[m]; !ok {
					return nil, p, fmt.Errorf("rule %s: mark %q not supported (circle, square, triangle, star, cross, x, diamond, hexagon, pentagon, arrow)", label, jr.Mark)
				}
				ps.Mark = m
				if ps.Fill == nil && ps.Stroke == nil {
					ps.Fill = &Fill{Color: "#3366cc"}
				}
			}
			r.Symbols = append(r.Symbols, ps)
		case "line":
			if strokeSym == nil && markerSym == nil {
				strokeSym = &Stroke{Color: "#3366cc", Width: 1}
			}
			if strokeSym != nil {
				if jr.CasingColor != "" {
					cc, err := color(jr.CasingColor, "")
					if err != nil {
						return nil, p, err
					}
					cw := 1.0
					if jr.CasingWidth != nil {
						cw = *jr.CasingWidth
					}
					casing := *strokeSym
					casing.Color, casing.Dash = cc, nil
					casing.Width = strokeSym.Width + 2*cw
					r.Symbols = append(r.Symbols, LineSym{Stroke: casing}) // first, so drawn underneath
				}
				r.Symbols = append(r.Symbols, LineSym{Stroke: *strokeSym})
			}
			if markerSym != nil {
				r.Symbols = append(r.Symbols, *markerSym)
			}
		case "polygon":
			ps := PolygonSym{Fill: fillSym, Stroke: strokeSym}
			if jr.FillPattern != "" && !strings.EqualFold(jr.FillPattern, "solid") {
				ps.Pattern = strings.ToLower(jr.FillPattern)
				if ps.PatternBackground, err = color(jr.PatternBackground, ""); err != nil {
					return nil, p, err
				}
				if jr.PatternSpacing != nil {
					ps.PatternSpacing = *jr.PatternSpacing
				}
				ps.PatternAngle = 45
				if jr.PatternAngle != nil {
					ps.PatternAngle = *jr.PatternAngle
				}
				if jr.PatternWidth != nil {
					ps.PatternWidth = *jr.PatternWidth
				}
			}
			r.Symbols = append(r.Symbols, ps)
			if markerSym != nil {
				r.Symbols = append(r.Symbols, *markerSym)
			}
		case "":
		default:
			return nil, p, fmt.Errorf(`rule %s: symbol must be "point", "line" or "polygon"`, label)
		}

		if jr.Label != "" {
			t := TextSym{Label: jr.Label, FontFamily: "sans", FontSize: 10, Color: "#000000", AnchorY: 0.5}
			if attrRe.MatchString(jr.Label) { // bare attribute name
				t.Label = "[" + jr.Label + "]"
			}
			if jr.FontFamily != "" {
				t.FontFamily = jr.FontFamily
			}
			if jr.FontSize != nil {
				t.FontSize = *jr.FontSize
			}
			t.Bold = strings.EqualFold(jr.FontWeight, "bold")
			t.Italic = strings.EqualFold(jr.FontStyle, "italic")
			if jr.LabelPosition != "" {
				pos, ok := labelPositions[strings.ToLower(jr.LabelPosition)]
				if !ok {
					return nil, p, fmt.Errorf("rule %s: label_position must be auto, center, n, ne, e, se, s, sw, w or nw", label)
				}
				t.Position = pos
			}
			if jr.LabelOffsetX != nil {
				t.DisplaceX = *jr.LabelOffsetX
			}
			if jr.LabelOffsetY != nil {
				t.DisplaceY = 0 - *jr.LabelOffsetY // TextSym follows SLD, where y points up
			}
			if jr.LabelAngle != nil {
				t.Rotation = strconv.FormatFloat(*jr.LabelAngle, 'f', -1, 64)
			}
			t.FollowLine = jr.LabelFollowLine
			t.Force = jr.LabelAllowOverlap
			if jr.LabelRepeat != nil {
				t.RepeatDistance = *jr.LabelRepeat
			}
			if jr.LabelPriority != nil {
				if *jr.LabelPriority < 1 || *jr.LabelPriority > 10 {
					return nil, p, fmt.Errorf("rule %s: label_priority must be 1 to 10", label)
				}
				t.Priority = *jr.LabelPriority
			}
			if t.Color, err = color(jr.LabelColor, "#000000"); err != nil {
				return nil, p, err
			}
			if jr.HaloColor != "" || jr.HaloRadius != nil {
				if t.HaloColor, err = color(jr.HaloColor, "#ffffff"); err != nil {
					return nil, p, err
				}
				t.HaloRadius = 1
				if jr.HaloRadius != nil {
					t.HaloRadius = *jr.HaloRadius
				}
			}
			r.Symbols = append(r.Symbols, t)
		}
		if len(r.Symbols) == 0 {
			return nil, p, fmt.Errorf("rule %s draws nothing: set icon/mark, fill, stroke or label", label)
		}
		st.Rules = append(st.Rules, r)
	}
	if len(p.missingIcons) > 0 {
		return nil, p, &MissingIconsError{Names: p.missingIcons}
	}
	return st, p, nil
}

func resolve(icons IconResolver, href string) (string, bool) {
	if icons == nil {
		return "", false
	}
	return icons(href)
}

func normColor(v string) (string, bool) {
	m := hexColorRe.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return "", false
	}
	return (&node{Text: "#" + m[1]}).normalizedColor(), true
}
