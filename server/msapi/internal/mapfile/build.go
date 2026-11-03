package mapfile

import (
	"bytes"
	"embed"
	"fmt"
	"strconv"
	"strings"
	"text/template"

	"msapi/internal/model"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"q":       Quote,
	"num":     num,
	"deref":   func(f *float64) float64 { return *f },
	"deref_i": func(i *int) int { return *i },
	"lower":   strings.ToLower,
	"indent": func(n int, s string) string {
		pad := strings.Repeat(" ", n)
		lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
		for i, l := range lines {
			if l != "" {
				lines[i] = pad + l
			}
		}
		return strings.Join(lines, "\n")
	},
}).ParseFS(templateFS, "templates/*.tmpl"))

// Map is everything needed to render services.map.
type Map struct {
	Title     string // ows_title; "" = "MapServer services"
	PublicURL string
	Fontset   bool // fonts/fontset.txt exists (labels need it)
	Config    []KV // MAP-level CONFIG (GDAL/S3 options)
	Layers    []Layer
}

type KV struct{ Key, Value string }

// Layer is one mapfile LAYER. Classes holds ready-made CLASS blocks (default
// style or compiled styles); ClassGroup selects the default style.
type Layer struct {
	Workspace  string // not rendered; selects the layer for ws_<name>.map
	Name       string
	Title      string
	Abstract   string
	Type       string // POINT | LINE | POLYGON | RASTER
	Connection string // "service=<store>" for PostGIS; empty for rasters
	Data       string
	// MapData, when set, is used for drawing instead of Data: geometry
	// simplified to the map's pixel size (see VectorMapData). WFS keeps Data.
	MapData    string
	GeomColumn string // excluded from GetFeatureInfo output when MapData is used

	// Set by expandLayers, not by callers.
	Group      string
	WMSOff     bool
	Projection string // "EPSG:4326" or "AUTO"
	SRS        string // ows_srs for this layer, e.g. "EPSG:32645 EPSG:4326 EPSG:3857"
	Extent     *model.Extent
	WFS        bool
	PK         string // wfs_featureid / gml id
	MinScale   *float64
	MaxScale   *float64
	Processing []string
	Offsite    string // e.g. "0 0 0" for rasters without alpha/nodata
	Opacity    *int   // layer OPACITY 0-100 (raster style opacity)
	ClassGroup string
	Classes    string
}

// Build renders the mapfile text.
func Build(m Map) (string, error) {
	m.Layers = expandLayers(m.Layers)
	var b bytes.Buffer
	if err := tmpl.ExecuteTemplate(&b, "map.tmpl", m); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Quote returns a mapfile double-quoted string. Line breaks become spaces.
func Quote(s string) string {
	s = strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// expandLayers turns a layer with MapData into what MapServer serves:
//   - without WFS: one layer that draws MapData;
//   - with WFS: a WMS group <name> holding a drawing layer <name>__map
//     (MapData), plus a WFS-only layer <name> with the full geometry, so
//     WMS and WFS clients both use <name>. (MapServer renames duplicate
//     layer names, so the pair cannot share one name.)
func expandLayers(in []Layer) []Layer {
	out := make([]Layer, 0, len(in))
	for _, l := range in {
		if l.MapData == "" {
			out = append(out, l)
			continue
		}
		draw := l
		draw.Data, draw.MapData = l.MapData, ""
		if !l.WFS {
			out = append(out, draw)
			continue
		}
		draw.Name, draw.Group, draw.WFS = l.Name+"__map", l.Name, false
		feat := l
		feat.MapData, feat.WMSOff, feat.Classes, feat.ClassGroup, feat.GeomColumn = "", true, "", "", ""
		out = append(out, draw, feat)
	}
	return out
}

// VectorMapData is the DATA used for drawing polygon and line layers: the
// geometry is simplified in PostGIS to about a quarter of a pixel, from the
// size of the requested box (!BOX!, in layer units), so a zoomed-out map
// does not pull every vertex over the network. Zoomed in, the tolerance
// becomes tiny and the shapes are exact.
func VectorMapData(src model.VectorSource) string {
	srid := src.SRID
	if src.DeclaredSRID {
		srid = src.ColumnSRID // the column's own SRID, for MapServer's bbox filter
	}
	return fmt.Sprintf(`mapnest_geom FROM (SELECT *, ST_Simplify("%s", (ST_XMax(!BOX!) - ST_XMin(!BOX!)) / 4096, true) AS mapnest_geom FROM "%s"."%s" WHERE "%s" && !BOX!) AS mapnest_sub USING UNIQUE %s USING SRID=%d`,
		src.GeomColumn, src.Schema, src.Table, src.GeomColumn, src.PK, srid)
}

// VectorData builds the PostGIS DATA string. MapServer 8 quotes the
// geometry and unique columns itself, so only schema and table are quoted
// here (names with quotes are rejected by inspect.CheckIdentifiers).
// USING SRID is the column's own SRID, which MapServer uses for its bbox
// filter: 0 for a declared SRID, whose real CRS goes in the layer PROJECTION.
func VectorData(src model.VectorSource) string {
	srid := src.SRID
	if src.DeclaredSRID {
		srid = src.ColumnSRID // the column's own SRID, for MapServer's bbox filter
	}
	return fmt.Sprintf(`%s FROM "%s"."%s" USING UNIQUE %s USING SRID=%d`,
		src.GeomColumn, src.Schema, src.Table, src.PK, srid)
}

// DefaultClasses is the style used when a layer has no style assigned, like
// GeoServer's default point/line/polygon styles.
func DefaultClasses(layerType string) string {
	switch layerType {
	case "POINT":
		return `CLASS
  NAME "default"
  STYLE
    SYMBOL "circle"
    SIZE 8
    COLOR 51 102 204
    OUTLINECOLOR 255 255 255
    WIDTH 1
  END
END`
	case "LINE":
		return `CLASS
  NAME "default"
  STYLE
    COLOR 51 102 204
    WIDTH 2
    LINECAP ROUND
    LINEJOIN ROUND
  END
END`
	case "POLYGON":
		return `CLASS
  NAME "default"
  STYLE
    COLOR 170 196 238
    OPACITY 70
  END
  STYLE
    OUTLINECOLOR 51 102 204
    WIDTH 1
  END
END`
	}
	return "" // rasters render without classes
}
