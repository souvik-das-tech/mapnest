package sld

// Style is the converter's intermediate form, independent of SLD/JSON and
// of the layer type.
type Style struct {
	Target  string // "vector" | "raster"
	Rules   []Rule
	Opacity *float64 // raster layer opacity (RasterSymbolizer/Opacity)
}

type Rule struct {
	Name     string
	Expr     string // mapfile EXPRESSION without outer parens check; "" = always
	Else     bool
	MinScale *float64
	MaxScale *float64
	Symbols  []Symbolizer
}

// Symbolizer is one of Point / Line / Polygon / Text / Raster.
type Symbolizer interface{ kind() string }

type Stroke struct {
	Color    string // "#rrggbb"
	Width    float64
	Opacity  *float64 // 0..1
	Dash     []float64
	LineCap  string
	LineJoin string
	Offset   float64 // perpendicular offset in px (right of the line direction)
}

type Fill struct {
	Color   string
	Opacity *float64
}

type PointSym struct {
	Mark     string // built-in symbol name, when not an icon
	Icon     string // symbol name of a PIXMAP icon ("icon_school")
	Size     string // number or "[attr]"; "" = default
	Rotation string
	Opacity  *float64
	Fill     *Fill
	Stroke   *Stroke
	OffsetX  float64 // px, right
	OffsetY  float64 // px, down
}

type LineSym struct{ Stroke Stroke }

// MarkerLineSym repeats a mark along lines or polygon outlines, rotated to
// follow the line (e.g. direction arrows).
type MarkerLineSym struct {
	Mark    string
	Size    float64
	Gap     float64 // distance between marks, px
	Color   string
	Opacity *float64
}

type PolygonSym struct {
	Fill   *Fill
	Stroke *Stroke
	// Pattern draws the fill as "hatch", "cross" (cross-hatch) or "dots"
	// in the fill colour; "" is a solid fill.
	Pattern           string
	PatternSpacing    float64 // px between lines / dots
	PatternAngle      float64 // degrees, hatch only
	PatternWidth      float64 // line width or dot size, px
	PatternBackground string  // optional solid colour under the pattern
}

type TextSym struct {
	Label      string // mapfile TEXT, e.g. "[name]"
	FontFamily string
	FontSize   float64
	Bold       bool
	Italic     bool
	Color      string
	HaloColor  string
	HaloRadius float64
	AnchorX    float64
	AnchorY    float64
	DisplaceX  float64
	DisplaceY  float64
	Rotation   string
	FollowLine bool
	LineOffset float64
	// Position overrides the anchor-derived POSITION ("auto", "uc", "cr", …).
	Position       string
	Force          bool    // draw even when it collides with other labels
	RepeatDistance float64 // px between repeated labels along a line
	Priority       int     // 1..10, higher wins collisions; 0 = default
}

type ColorEntry struct {
	Color    string
	Quantity float64
	Opacity  *float64
	Label    string
}

type RasterSym struct {
	MapType string // ramp | intervals | values
	Entries []ColorEntry
}

func (PointSym) kind() string      { return "point" }
func (LineSym) kind() string       { return "line" }
func (MarkerLineSym) kind() string { return "line" }
func (PolygonSym) kind() string    { return "polygon" }
func (TextSym) kind() string       { return "text" }
func (RasterSym) kind() string     { return "raster" }
