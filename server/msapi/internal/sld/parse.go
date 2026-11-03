package sld

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// IconResolver maps an ExternalGraphic href to a mapfile symbol name.
type IconResolver func(href string) (symbol string, ok bool)

type parser struct {
	icons        IconResolver
	warnings     []string
	missingIcons []string
	usedIcons    map[string]bool
}

func (p *parser) warn(format string, args ...any) {
	p.warnings = append(p.warnings, fmt.Sprintf(format, args...))
}

// parseSLD reads the first (or the default) UserStyle of an SLD document.
func parseSLD(data []byte, icons IconResolver) (*Style, *parser, error) {
	root, err := parseXML(data)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid XML: %w", err)
	}
	p := &parser{icons: icons, usedIcons: map[string]bool{}}

	var styles []*node
	switch root.name() {
	case "StyledLayerDescriptor":
		for _, layerKind := range []string{"NamedLayer", "UserLayer"} {
			for _, l := range root.children(layerKind) {
				styles = append(styles, l.children("UserStyle")...)
			}
		}
	case "UserStyle":
		styles = []*node{root}
	case "FeatureTypeStyle", "CoverageStyle":
		styles = []*node{{Nodes: []node{*root}}}
	default:
		return nil, nil, fmt.Errorf("root element %s is not an SLD", root.name())
	}
	if len(styles) == 0 {
		return nil, nil, fmt.Errorf("no UserStyle found")
	}
	us := styles[0]
	for _, s := range styles {
		if v := s.child("IsDefault").text(); v == "1" || strings.EqualFold(v, "true") {
			us = s
			break
		}
	}
	if len(styles) > 1 {
		p.warn("document has %d UserStyles; only %q is used", len(styles), us.child("Name").text())
	}

	st := &Style{Target: "vector"}
	fts := append(us.children("FeatureTypeStyle"), us.children("CoverageStyle")...)
	if len(fts) > 1 {
		p.warn("%d FeatureTypeStyles are merged into one list of rules; MapServer draws the first matching rule per feature", len(fts))
	}
	hasRaster, hasVector := false, false
	for _, ft := range fts {
		for _, c := range ft.Nodes {
			switch c.name() {
			case "Rule", "Name", "Title", "Abstract", "Description", "FeatureTypeName", "SemanticTypeIdentifier", "CoverageName":
			case "Transformation":
				p.warn("rendering transformations are not supported (ignored)")
			case "VendorOption":
				p.warn("FeatureTypeStyle VendorOption %q ignored", c.attr("name"))
			default:
				p.warn("FeatureTypeStyle element %s ignored", c.name())
			}
		}
		for _, rn := range ft.children("Rule") {
			r, ok := p.rule(rn)
			if !ok {
				continue
			}
			for _, s := range r.Symbols {
				if s.kind() == "raster" {
					hasRaster = true
				} else {
					hasVector = true
				}
			}
			if rs, ok := rasterOpacity(rn); ok {
				st.Opacity = &rs
			}
			st.Rules = append(st.Rules, r)
		}
	}
	if hasRaster && hasVector {
		return nil, p, fmt.Errorf("a style cannot mix RasterSymbolizer with vector symbolizers")
	}
	if hasRaster {
		st.Target = "raster"
	}
	if len(p.missingIcons) > 0 {
		return nil, p, &MissingIconsError{Names: p.missingIcons}
	}
	return st, p, nil
}

// MissingIconsError lists ExternalGraphic references with no uploaded icon.
type MissingIconsError struct{ Names []string }

func (e *MissingIconsError) Error() string {
	return "unknown icon(s): " + strings.Join(e.Names, ", ") + " (upload them with POST /icons first)"
}

func rasterOpacity(rule *node) (float64, bool) {
	rs := rule.child("RasterSymbolizer")
	if rs == nil || rs.child("Opacity") == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(rs.child("Opacity").text(), 64)
	return v, err == nil
}

func (p *parser) rule(n *node) (Rule, bool) {
	// Legend label: Title (SLD 1.0), Description/Title (SE 1.1), then Name.
	r := Rule{Name: n.child("Title").text()}
	if r.Name == "" {
		r.Name = n.path("Description", "Title").text()
	}
	if r.Name == "" {
		r.Name = n.child("Name").text()
	}
	label := r.Name
	if label == "" {
		label = "(unnamed)"
	}

	for i := range n.Nodes {
		c := &n.Nodes[i]
		switch c.name() {
		case "Name", "Title", "Abstract", "Description", "LegendGraphic":
		case "Filter":
			expr, err := filterExpr(c)
			if err != nil {
				p.warn("rule %s skipped: %v", label, err)
				return r, false
			}
			r.Expr = expr
		case "ElseFilter":
			r.Else = true
		case "MinScaleDenominator":
			if v, err := strconv.ParseFloat(c.text(), 64); err == nil {
				r.MinScale = &v
			}
		case "MaxScaleDenominator":
			if v, err := strconv.ParseFloat(c.text(), 64); err == nil {
				r.MaxScale = &v
			}
		case "PointSymbolizer":
			if s, ok := p.point(c, label); ok {
				r.Symbols = append(r.Symbols, s)
			}
		case "LineSymbolizer":
			p.geometryWarn(c, label)
			stroke := p.stroke(c.child("Stroke"), label)
			if stroke == nil {
				stroke = &Stroke{Color: "#000000", Width: 1}
			}
			if c.child("PerpendicularOffset") != nil {
				p.warn("rule %s: LineSymbolizer PerpendicularOffset ignored", label)
			}
			r.Symbols = append(r.Symbols, LineSym{Stroke: *stroke})
		case "PolygonSymbolizer":
			p.geometryWarn(c, label)
			ps := PolygonSym{Fill: p.fill(c.child("Fill"), label), Stroke: p.stroke(c.child("Stroke"), label)}
			if c.child("Fill") == nil && c.child("Stroke") == nil {
				ps.Fill = &Fill{Color: "#808080"} // SLD default fill
			}
			for _, x := range []string{"Displacement", "PerpendicularOffset", "VendorOption"} {
				if c.child(x) != nil {
					p.warn("rule %s: PolygonSymbolizer %s ignored", label, x)
				}
			}
			r.Symbols = append(r.Symbols, ps)
		case "TextSymbolizer":
			if s, ok := p.text(c, label); ok {
				r.Symbols = append(r.Symbols, s)
			}
		case "RasterSymbolizer":
			if s, ok := p.raster(c, label); ok {
				r.Symbols = append(r.Symbols, s)
			}
		default:
			p.warn("rule %s: element %s ignored", label, c.name())
		}
	}
	if r.Else && r.Expr != "" {
		p.warn("rule %s has both Filter and ElseFilter; ElseFilter wins", label)
		r.Expr = ""
	}
	if len(r.Symbols) == 0 {
		p.warn("rule %s has no supported symbolizer; skipped", label)
		return r, false
	}
	return r, true
}

func (p *parser) geometryWarn(n *node, label string) {
	if n.child("Geometry") != nil {
		p.warn("rule %s: %s Geometry element ignored (the layer geometry is used)", label, n.name())
	}
}

var hexColorRe = regexp.MustCompile(`^#?([0-9a-fA-F]{6}|[0-9a-fA-F]{3})$`)

func (p *parser) color(n *node, def, label string) string {
	if n == nil {
		return def
	}
	v, dyn := n.expr()
	if dyn {
		p.warn("rule %s: dynamic colour %s not supported; using %s", label, v, def)
		return def
	}
	m := hexColorRe.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		p.warn("rule %s: colour %q not understood; using %s", label, v, def)
		return def
	}
	h := strings.ToLower(m[1])
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	return "#" + h
}

func (p *parser) number(n *node, label, what string) (float64, bool) {
	if n == nil {
		return 0, false
	}
	v, dyn := n.expr()
	if dyn {
		p.warn("rule %s: dynamic %s %s not supported; using default", label, what, v)
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		p.warn("rule %s: %s %q is not a number; using default", label, what, v)
		return 0, false
	}
	return f, true
}

func (p *parser) opacity(n *node, label string) *float64 {
	if v, ok := p.number(n, label, "opacity"); ok {
		v = clamp01(v)
		return &v
	}
	return nil
}

// sizeOrAttr allows "[attr]" bindings (mapfile supports SIZE [attr], ANGLE [attr]).
func (p *parser) sizeOrAttr(n *node, label, what string) string {
	if n == nil {
		return ""
	}
	v, dyn := n.expr()
	v = strings.TrimSpace(v)
	if dyn {
		if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") && attrRe.MatchString(v[1:len(v)-1]) {
			return v
		}
		p.warn("rule %s: %s expression %s not supported; using default", label, what, v)
		return ""
	}
	if _, err := strconv.ParseFloat(v, 64); err != nil {
		p.warn("rule %s: %s %q is not a number; using default", label, what, v)
		return ""
	}
	return v
}

func (p *parser) fill(n *node, label string) *Fill {
	if n == nil {
		return nil
	}
	if n.child("GraphicFill") != nil {
		p.warn("rule %s: GraphicFill (pattern fill) not supported; using a solid fill", label)
	}
	ps := n.params()
	return &Fill{Color: p.color(ps["fill"], "#808080", label), Opacity: p.opacity(ps["fill-opacity"], label)}
}

func (p *parser) stroke(n *node, label string) *Stroke {
	if n == nil {
		return nil
	}
	for _, x := range []string{"GraphicFill", "GraphicStroke"} {
		if n.child(x) != nil {
			p.warn("rule %s: Stroke %s not supported; drawing a plain line", label, x)
		}
	}
	ps := n.params()
	s := &Stroke{Color: p.color(ps["stroke"], "#000000", label), Width: 1}
	if w, ok := p.number(ps["stroke-width"], label, "stroke-width"); ok {
		s.Width = w
	}
	s.Opacity = p.opacity(ps["stroke-opacity"], label)
	if d := ps["stroke-dasharray"]; d != nil {
		v, _ := d.expr()
		for _, f := range strings.Fields(strings.ReplaceAll(v, ",", " ")) {
			if x, err := strconv.ParseFloat(f, 64); err == nil {
				s.Dash = append(s.Dash, x)
			}
		}
		if len(s.Dash)%2 == 1 { // SVG repeats odd lists
			s.Dash = append(s.Dash, s.Dash...)
		}
	}
	if c := ps["stroke-linecap"]; c != nil {
		s.LineCap = strings.ToLower(c.text())
	}
	if j := ps["stroke-linejoin"]; j != nil {
		s.LineJoin = strings.ToLower(j.text())
		if s.LineJoin == "mitre" {
			s.LineJoin = "miter"
		}
	}
	if ps["stroke-dashoffset"] != nil {
		p.warn("rule %s: stroke-dashoffset ignored", label)
	}
	return s
}

var marks = map[string]string{
	"circle": "circle", "square": "square", "triangle": "triangle",
	"star": "star", "cross": "cross", "x": "x",
	"diamond": "diamond", "hexagon": "hexagon", "pentagon": "pentagon", "arrow": "arrow",
}

func (p *parser) point(n *node, label string) (Symbolizer, bool) {
	p.geometryWarn(n, label)
	g := n.child("Graphic")
	if g == nil {
		p.warn("rule %s: PointSymbolizer without Graphic skipped", label)
		return nil, false
	}
	s := PointSym{
		Size:     p.sizeOrAttr(g.child("Size"), label, "size"),
		Rotation: p.sizeOrAttr(g.child("Rotation"), label, "rotation"),
		Opacity:  p.opacity(g.child("Opacity"), label),
	}
	for _, x := range []string{"AnchorPoint", "Displacement"} {
		if g.child(x) != nil {
			p.warn("rule %s: Graphic %s ignored", label, x)
		}
	}
	// First usable ExternalGraphic or Mark wins (SLD fallback order).
	for i := range g.Nodes {
		c := &g.Nodes[i]
		switch c.name() {
		case "ExternalGraphic":
			href := c.child("OnlineResource").attr("href")
			if href == "" {
				href = c.child("OnlineResource").text()
			}
			if c.child("InlineContent") != nil {
				p.warn("rule %s: inline ExternalGraphic not supported", label)
				continue
			}
			if p.icons == nil {
				p.missingIcons = append(p.missingIcons, href)
				return nil, false
			}
			sym, ok := p.icons(href)
			if !ok {
				p.missingIcons = append(p.missingIcons, href)
				return nil, false
			}
			p.usedIcons[sym] = true
			s.Icon = sym
			return s, true
		case "Mark":
			wkn := strings.ToLower(c.child("WellKnownName").text())
			if wkn == "" {
				wkn = "square"
			}
			m, ok := marks[wkn]
			if !ok {
				p.warn("rule %s: mark %q not supported; using square", label, wkn)
				m = "square"
			}
			s.Mark = m
			s.Fill = p.fill(c.child("Fill"), label)
			s.Stroke = p.stroke(c.child("Stroke"), label)
			if c.child("Fill") == nil && c.child("Stroke") == nil { // SLD defaults
				s.Fill = &Fill{Color: "#808080"}
				s.Stroke = &Stroke{Color: "#000000", Width: 1}
			}
			return s, true
		}
	}
	// No Mark/ExternalGraphic: SLD default is a gray square.
	s.Mark = "square"
	s.Fill = &Fill{Color: "#808080"}
	s.Stroke = &Stroke{Color: "#000000", Width: 1}
	return s, true
}

func (p *parser) text(n *node, label string) (Symbolizer, bool) {
	p.geometryWarn(n, label)
	lbl := n.child("Label")
	if lbl == nil {
		p.warn("rule %s: TextSymbolizer without Label skipped", label)
		return nil, false
	}
	text, _ := lbl.expr()
	if text == "" {
		p.warn("rule %s: empty Label skipped", label)
		return nil, false
	}
	t := TextSym{Label: text, FontFamily: "sans", FontSize: 10, Color: "#000000", AnchorX: 0, AnchorY: 0.5}

	fp := n.child("Font").params()
	if f := fp["font-family"]; f != nil {
		t.FontFamily = f.text()
	}
	if v, ok := p.number(fp["font-size"], label, "font-size"); ok {
		t.FontSize = v
	}
	t.Bold = strings.EqualFold(fp["font-weight"].text(), "bold")
	t.Italic = strings.EqualFold(fp["font-style"].text(), "italic") || strings.EqualFold(fp["font-style"].text(), "oblique")

	if f := n.child("Fill"); f != nil {
		t.Color = p.color(f.params()["fill"], "#000000", label)
	}
	if h := n.child("Halo"); h != nil {
		t.HaloRadius = 1
		if r, ok := p.number(h.child("Radius"), label, "halo radius"); ok {
			t.HaloRadius = r
		}
		t.HaloColor = "#ffffff"
		if f := h.child("Fill"); f != nil {
			t.HaloColor = p.color(f.params()["fill"], "#ffffff", label)
		}
	}
	if lp := n.child("LabelPlacement"); lp != nil {
		if pp := lp.child("PointPlacement"); pp != nil {
			if a := pp.child("AnchorPoint"); a != nil {
				if x, ok := p.number(a.child("AnchorPointX"), label, "anchor x"); ok {
					t.AnchorX = x
				}
				if y, ok := p.number(a.child("AnchorPointY"), label, "anchor y"); ok {
					t.AnchorY = y
				}
			}
			if d := pp.child("Displacement"); d != nil {
				t.DisplaceX, _ = p.number(d.child("DisplacementX"), label, "displacement x")
				t.DisplaceY, _ = p.number(d.child("DisplacementY"), label, "displacement y")
			}
			t.Rotation = p.sizeOrAttr(pp.child("Rotation"), label, "label rotation")
		}
		if l := lp.child("LinePlacement"); l != nil {
			t.FollowLine = true
			t.LineOffset, _ = p.number(l.child("PerpendicularOffset"), label, "perpendicular offset")
		}
	}
	for _, x := range n.children("VendorOption") {
		p.warn("rule %s: TextSymbolizer VendorOption %q ignored", label, x.attr("name"))
	}
	for _, x := range []string{"Priority", "Graphic"} {
		if n.child(x) != nil {
			p.warn("rule %s: TextSymbolizer %s ignored", label, x)
		}
	}
	return t, true
}

func (p *parser) raster(n *node, label string) (Symbolizer, bool) {
	for _, x := range []string{"ChannelSelection", "ContrastEnhancement", "ShadedRelief", "ImageOutline", "OverlapBehavior"} {
		if n.child(x) != nil {
			p.warn("rule %s: RasterSymbolizer %s ignored", label, x)
		}
	}
	cm := n.child("ColorMap")
	if cm == nil {
		// Opacity only: default rendering.
		return RasterSym{}, true
	}
	t := strings.ToLower(cm.attr("type"))
	if t == "" {
		t = "ramp"
	}
	if t != "ramp" && t != "intervals" && t != "values" {
		p.warn("rule %s: ColorMap type %q not supported; using ramp", label, t)
		t = "ramp"
	}
	rs := RasterSym{MapType: t}
	for _, e := range cm.children("ColorMapEntry") {
		q, err := strconv.ParseFloat(e.attr("quantity"), 64)
		if err != nil {
			p.warn("rule %s: ColorMapEntry without numeric quantity skipped", label)
			continue
		}
		ce := ColorEntry{Quantity: q, Label: e.attr("label"), Color: "#000000"}
		if m := hexColorRe.FindStringSubmatch(e.attr("color")); m != nil {
			ce.Color = (&node{Text: "#" + m[1]}).normalizedColor()
		}
		if o := e.attr("opacity"); o != "" {
			if v, err := strconv.ParseFloat(o, 64); err == nil {
				v = clamp01(v)
				ce.Opacity = &v
			}
		}
		rs.Entries = append(rs.Entries, ce)
	}
	if len(rs.Entries) == 0 {
		p.warn("rule %s: ColorMap has no entries", label)
		return RasterSym{}, true
	}
	return rs, true
}

func (n *node) normalizedColor() string {
	m := hexColorRe.FindStringSubmatch(strings.TrimSpace(n.Text))
	if m == nil {
		return "#000000"
	}
	h := strings.ToLower(m[1])
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	return "#" + h
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
