package sld

import (
	"errors"
	"sort"
)

// ErrRendersNothing: the style has no usable rule (PLAN.md §6.3: 422).
var ErrRendersNothing = errors.New("the style would render nothing (no supported rules)")

// Parsed is a style read from SLD or JSON, ready to render for any layer.
type Parsed struct {
	style     *Style
	Target    string   // "vector" | "raster"
	Warnings  []string // unsupported parts found while parsing
	UsedIcons []string // icon symbol names referenced
	Opacity   *float64 // raster layer opacity, if set
}

// Parse reads a style. format is "sld" or "json". icons resolves
// ExternalGraphic / "icon" references to symbol names.
func Parse(format string, body []byte, icons IconResolver) (*Parsed, error) {
	var (
		st  *Style
		p   *parser
		err error
	)
	switch format {
	case "sld":
		st, p, err = parseSLD(body, icons)
	case "json":
		st, p, err = parseJSONStyle(body, icons)
	default:
		return nil, errors.New(`format must be "sld" or "json"`)
	}
	if err != nil {
		return nil, err
	}
	if len(st.Rules) == 0 {
		return nil, ErrRendersNothing
	}
	out := &Parsed{style: st, Target: st.Target, Warnings: p.warnings, Opacity: st.Opacity}
	for sym := range p.usedIcons {
		out.UsedIcons = append(out.UsedIcons, sym)
	}
	sort.Strings(out.UsedIcons)
	return out, nil
}

// Render produces the CLASS blocks for a layer of the given type, grouped
// under the style name. Warnings are about this layer type only.
func (p *Parsed) Render(group, layerType string) (classes string, warnings []string) {
	return render(p.style, group, layerType)
}
