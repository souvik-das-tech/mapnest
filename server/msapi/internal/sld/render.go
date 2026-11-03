package sld

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// render writes CLASS blocks for one layer type. group is the style name
// (CLASS GROUP, selectable with WMS STYLES=). layerType is POINT, LINE,
// POLYGON, RASTER or "" (generic, used for the stored preview).
func render(st *Style, group, layerType string) (string, []string) {
	r := &renderer{b: &strings.Builder{}, group: group, layerType: layerType}
	if st.Target == "raster" {
		for _, rule := range st.Rules {
			for _, s := range rule.Symbols {
				if rs, ok := s.(RasterSym); ok && len(rs.Entries) > 0 {
					r.raster(rs)
				}
			}
		}
		return r.b.String(), r.warnings
	}

	// Rules with a filter (or none) keep their order; ElseFilter rules go last,
	// because MapServer uses the first CLASS that matches.
	rules := append([]Rule(nil), st.Rules...)
	sort.SliceStable(rules, func(i, j int) bool { return !rules[i].Else && rules[j].Else })
	for i, rule := range rules {
		r.vectorRule(rule, i)
	}
	return r.b.String(), r.warnings
}

type renderer struct {
	b         *strings.Builder
	group     string
	layerType string
	warnings  []string
}

func (r *renderer) line(indent int, format string, args ...any) {
	r.b.WriteString(strings.Repeat("  ", indent))
	fmt.Fprintf(r.b, format, args...)
	r.b.WriteByte('\n')
}

func (r *renderer) warn(format string, args ...any) {
	r.warnings = append(r.warnings, fmt.Sprintf(format, args...))
}

func (r *renderer) classHeader(name string, expr string, minScale, maxScale *float64) {
	r.line(0, "CLASS")
	r.line(1, "NAME %s", mq(name))
	if r.group != "" {
		r.line(1, "GROUP %s", mq(r.group))
	}
	if expr != "" {
		r.line(1, "EXPRESSION %s", expr)
	}
	if minScale != nil {
		r.line(1, "MINSCALEDENOM %s", num(*minScale))
	}
	if maxScale != nil {
		r.line(1, "MAXSCALEDENOM %s", num(*maxScale))
	}
}

func (r *renderer) vectorRule(rule Rule, i int) {
	name := rule.Name
	if name == "" {
		name = "rule " + strconv.Itoa(i+1)
	}
	// Render the symbolizers first: a rule that draws nothing on this layer
	// type gets no CLASS at all.
	outer := r.b
	body := &strings.Builder{}
	r.b = body
	for _, s := range rule.Symbols {
		switch s := s.(type) {
		case PointSym:
			r.point(s)
		case LineSym:
			r.lineSym(s, name)
		case MarkerLineSym:
			r.markerLine(s, name)
		case PolygonSym:
			r.polygon(s, name)
		case TextSym:
			r.text(s)
		}
	}
	r.b = outer
	if body.Len() == 0 {
		r.warn("rule %s draws nothing on a %s layer; skipped", name, strings.ToLower(r.layerType))
		return
	}
	r.classHeader(name, rule.Expr, rule.MinScale, rule.MaxScale)
	r.b.WriteString(body.String())
	r.line(0, "END")
}

func (r *renderer) point(s PointSym) {
	r.line(1, "STYLE")
	if s.Icon != "" {
		r.line(2, "SYMBOL %s", mq(s.Icon))
	} else {
		r.line(2, "SYMBOL %s", mq(s.Mark))
	}
	switch {
	case s.Size != "":
		r.line(2, "SIZE %s", s.Size)
	case s.Icon == "":
		r.line(2, "SIZE 6") // SLD default
	}
	if s.Icon == "" {
		if s.Fill != nil {
			r.line(2, "COLOR %s", mq(withAlpha(s.Fill.Color, s.Fill.Opacity)))
		}
		if s.Stroke != nil {
			r.line(2, "OUTLINECOLOR %s", mq(withAlpha(s.Stroke.Color, s.Stroke.Opacity)))
			r.line(2, "WIDTH %s", num(s.Stroke.Width))
		}
	}
	if s.Rotation != "" {
		r.line(2, "ANGLE %s", s.Rotation)
	}
	if s.Opacity != nil {
		r.line(2, "OPACITY %d", pct(*s.Opacity))
	}
	if s.OffsetX != 0 || s.OffsetY != 0 {
		r.line(2, "OFFSET %s %s", num(s.OffsetX), num(s.OffsetY))
	}
	if r.layerType == "LINE" || r.layerType == "POLYGON" {
		// GeoServer draws point symbolizers on lines/polygons at the centroid.
		r.line(2, `GEOMTRANSFORM "centroid"`)
	}
	r.line(1, "END")
}

func (r *renderer) strokeAttrs(s Stroke) {
	r.line(2, "WIDTH %s", num(s.Width))
	if len(s.Dash) > 0 {
		parts := make([]string, len(s.Dash))
		for i, d := range s.Dash {
			parts[i] = num(d)
		}
		r.line(2, "PATTERN %s END", strings.Join(parts, " "))
	}
	if s.LineCap != "" {
		r.line(2, "LINECAP %s", strings.ToUpper(s.LineCap))
	}
	if s.LineJoin != "" {
		r.line(2, "LINEJOIN %s", strings.ToUpper(s.LineJoin))
	}
	if s.Offset != 0 {
		r.line(2, "OFFSET %s -99", num(s.Offset)) // -99: perpendicular to the line
	}
}

func (r *renderer) markerLine(s MarkerLineSym, rule string) {
	if r.layerType == "POINT" {
		r.warn("rule %s: line markers have no effect on a point layer", rule)
		return
	}
	r.line(1, "STYLE")
	r.line(2, "SYMBOL %s", mq(s.Mark))
	r.line(2, "SIZE %s", num(s.Size))
	// On polygon layers COLOR would tile the fill; OUTLINECOLOR follows the ring.
	attr := "COLOR"
	if r.layerType == "POLYGON" {
		attr = "OUTLINECOLOR"
	}
	r.line(2, "%s %s", attr, mq(s.Color))
	r.line(2, "GAP %s", num(0-s.Gap)) // negative: rotate marks along the line
	if s.Opacity != nil {
		r.line(2, "OPACITY %d", pct(*s.Opacity))
	}
	r.line(1, "END")
}

func (r *renderer) lineSym(s LineSym, rule string) {
	switch r.layerType {
	case "POINT":
		r.warn("rule %s: LineSymbolizer has no effect on a point layer", rule)
	case "POLYGON":
		r.line(1, "STYLE")
		r.line(2, "OUTLINECOLOR %s", mq(withAlpha(s.Stroke.Color, s.Stroke.Opacity)))
		r.strokeAttrs(s.Stroke)
		r.line(1, "END")
	default:
		r.line(1, "STYLE")
		r.line(2, "COLOR %s", mq(withAlpha(s.Stroke.Color, s.Stroke.Opacity)))
		r.strokeAttrs(s.Stroke)
		r.line(1, "END")
	}
}

func (r *renderer) polygon(s PolygonSym, rule string) {
	if r.layerType == "POINT" || r.layerType == "LINE" {
		r.warn("rule %s: PolygonSymbolizer has no effect on a %s layer", rule, strings.ToLower(r.layerType))
		return
	}
	if s.Fill != nil {
		color := mq(withAlpha(s.Fill.Color, s.Fill.Opacity))
		if s.Pattern != "" && s.PatternBackground != "" {
			r.line(1, "STYLE")
			r.line(2, "COLOR %s", mq(withAlpha(s.PatternBackground, s.Fill.Opacity)))
			r.line(1, "END")
		}
		hatch := func(angle float64) {
			r.line(1, "STYLE")
			r.line(2, `SYMBOL "hatch"`)
			r.line(2, "COLOR %s", color)
			r.line(2, "SIZE %s", num(orDefault(s.PatternSpacing, 8)))
			r.line(2, "WIDTH %s", num(orDefault(s.PatternWidth, 1)))
			r.line(2, "ANGLE %s", num(angle))
			r.line(1, "END")
		}
		switch s.Pattern {
		case "hatch":
			hatch(s.PatternAngle)
		case "cross":
			hatch(s.PatternAngle)
			hatch(s.PatternAngle + 90)
		case "dots":
			size := orDefault(s.PatternWidth, 3)
			r.line(1, "STYLE")
			r.line(2, `SYMBOL "circle"`)
			r.line(2, "COLOR %s", color)
			r.line(2, "SIZE %s", num(size))
			r.line(2, "GAP %s", num(orDefault(s.PatternSpacing, 8)+size))
			r.line(1, "END")
		default:
			r.line(1, "STYLE")
			r.line(2, "COLOR %s", color)
			r.line(1, "END")
		}
	}
	if s.Stroke != nil {
		r.line(1, "STYLE")
		r.line(2, "OUTLINECOLOR %s", mq(withAlpha(s.Stroke.Color, s.Stroke.Opacity)))
		r.strokeAttrs(*s.Stroke)
		r.line(1, "END")
	}
}

func (r *renderer) text(t TextSym) {
	r.line(1, "LABEL")
	r.line(2, "TEXT %s", mq(t.Label))
	r.line(2, "FONT %s", mq(FontName(t.FontFamily, t.Bold, t.Italic)))
	r.line(2, "SIZE %s", num(t.FontSize))
	r.line(2, "COLOR %s", mq(t.Color))
	if t.HaloColor != "" && t.HaloRadius > 0 {
		r.line(2, "OUTLINECOLOR %s", mq(t.HaloColor))
		r.line(2, "OUTLINEWIDTH %s", num(t.HaloRadius))
	}
	if t.FollowLine {
		r.line(2, "ANGLE FOLLOW")
		if t.LineOffset != 0 {
			r.line(2, "OFFSET 0 %s", num(0-t.LineOffset))
		}
	} else {
		pos := t.Position
		if pos == "" {
			pos = position(t.AnchorX, t.AnchorY)
		}
		r.line(2, "POSITION %s", pos)
		if t.DisplaceX != 0 || t.DisplaceY != 0 {
			// SLD displacement y points up; mapfile y points down.
			r.line(2, "OFFSET %s %s", num(t.DisplaceX), num(0-t.DisplaceY)) // 0-x avoids "-0"
		}
		if t.Rotation != "" {
			r.line(2, "ANGLE %s", t.Rotation)
		}
	}
	if t.RepeatDistance > 0 {
		r.line(2, "REPEATDISTANCE %s", num(t.RepeatDistance))
	}
	if t.Priority > 0 {
		r.line(2, "PRIORITY %d", t.Priority)
	}
	if t.Force {
		r.line(2, "FORCE TRUE")
	}
	r.line(2, "PARTIALS FALSE")
	r.line(1, "END")
}

func orDefault(v, def float64) float64 {
	if v == 0 {
		return def
	}
	return v
}

// position maps an SLD AnchorPoint (where on the label the point sits) to a
// MapServer POSITION (where the label sits relative to the point).
func position(ax, ay float64) string {
	y := "c"
	switch {
	case ay <= 0.25:
		y = "u" // anchor at label bottom → label above the point
	case ay >= 0.75:
		y = "l"
	}
	x := "c"
	switch {
	case ax <= 0.25:
		x = "r" // anchor at label left → label right of the point
	case ax >= 0.75:
		x = "l"
	}
	return y + x
}

func (r *renderer) raster(rs RasterSym) {
	es := append([]ColorEntry(nil), rs.Entries...)
	sort.SliceStable(es, func(i, j int) bool { return es[i].Quantity < es[j].Quantity })
	transparent := func(e ColorEntry) bool { return e.Opacity != nil && *e.Opacity == 0 }
	name := func(e ColorEntry, def string) string {
		if e.Label != "" {
			return e.Label
		}
		return def
	}

	switch rs.MapType {
	case "values":
		for _, e := range es {
			if transparent(e) {
				continue
			}
			r.classHeader(name(e, num(e.Quantity)), fmt.Sprintf("([pixel] = %s)", num(e.Quantity)), nil, nil)
			r.line(1, "STYLE")
			r.line(2, "COLOR %s", mq(e.Color))
			r.line(1, "END")
			r.line(0, "END")
		}
	case "intervals":
		for i, e := range es {
			if transparent(e) {
				continue
			}
			expr := fmt.Sprintf("([pixel] < %s)", num(e.Quantity))
			if i > 0 {
				expr = fmt.Sprintf("([pixel] >= %s AND [pixel] < %s)", num(es[i-1].Quantity), num(e.Quantity))
			}
			r.classHeader(name(e, "< "+num(e.Quantity)), expr, nil, nil)
			r.line(1, "STYLE")
			r.line(2, "COLOR %s", mq(e.Color))
			r.line(1, "END")
			r.line(0, "END")
		}
	default: // ramp
		if len(es) == 1 {
			e := es[0]
			r.classHeader(name(e, num(e.Quantity)), fmt.Sprintf("([pixel] = %s)", num(e.Quantity)), nil, nil)
			r.line(1, "STYLE")
			r.line(2, "COLOR %s", mq(e.Color))
			r.line(1, "END")
			r.line(0, "END")
			return
		}
		for i := 0; i+1 < len(es); i++ {
			a, b := es[i], es[i+1]
			// A fully transparent end (e.g. no-data at -9999) hides the whole
			// segment; interpolating opacity is not possible with COLORRANGE.
			if transparent(a) || transparent(b) {
				continue
			}
			if a.Opacity != nil && *a.Opacity > 0 && *a.Opacity < 1 || b.Opacity != nil && *b.Opacity > 0 && *b.Opacity < 1 {
				r.warn("ColorMap entry opacity between 0 and 1 is not supported in ramps; drawn opaque")
			}
			op := "<"
			if i+2 == len(es) {
				op = "<=" // include the top value
			}
			expr := fmt.Sprintf("([pixel] >= %s AND [pixel] %s %s)", num(a.Quantity), op, num(b.Quantity))
			r.classHeader(name(a, num(a.Quantity)+" – "+num(b.Quantity)), expr, nil, nil)
			r.line(1, "STYLE")
			r.line(2, "COLORRANGE %s %s", rgb(a.Color), rgb(b.Color))
			r.line(2, "DATARANGE %s %s", num(a.Quantity), num(b.Quantity))
			r.line(1, "END")
			r.line(0, "END")
		}
	}
}

// FontName maps an SLD font family to a fontset alias (fonts.go).
func FontName(family string, bold, italic bool) string {
	f := strings.ToLower(family)
	base := "sans"
	switch {
	case strings.Contains(f, "mono") || strings.Contains(f, "courier") || strings.Contains(f, "consol"):
		base = "mono"
	case strings.Contains(f, "serif") && !strings.Contains(f, "sans"),
		strings.Contains(f, "times"), strings.Contains(f, "georgia"):
		base = "serif"
	}
	switch {
	case bold && italic:
		return base + "-bold-italic"
	case bold:
		return base + "-bold"
	case italic:
		return base + "-italic"
	}
	return base
}

// withAlpha returns "#rrggbb" or "#rrggbbaa" when an opacity below 1 is set.
func withAlpha(color string, opacity *float64) string {
	if opacity == nil || *opacity >= 1 {
		return color
	}
	return fmt.Sprintf("%s%02x", color, int(math.Round(*opacity*255)))
}

func rgb(hex string) string {
	v, err := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	if err != nil {
		return "0 0 0"
	}
	return fmt.Sprintf("%d %d %d", v>>16&0xff, v>>8&0xff, v&0xff)
}

func pct(f float64) int { return int(math.Round(clamp01(f) * 100)) }

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// mq quotes a mapfile string.
func mq(s string) string {
	s = strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
