package model

import "errors"

const (
	KindVector = "vector"
	KindRaster = "raster"
)

// VectorSource is layers.source for a PostGIS layer.
type VectorSource struct {
	Schema     string `json:"schema"`
	Table      string `json:"table"`
	GeomColumn string `json:"geom_column"`
	PK         string `json:"pk"`
	GeomType   string `json:"geom_type"`  // PostGIS type, e.g. MULTIPOLYGON
	LayerType  string `json:"layer_type"` // mapfile TYPE: POINT | LINE | POLYGON
	SRID       int    `json:"srid"`
	// DeclaredSRID: SRID was chosen by the user (at publish or later), like
	// GeoServer's "Declared SRS" with "force declared". ColumnSRID is the
	// column's own SRID (0 when it has none, common after a QGIS import).
	// The database is not changed; MapServer queries with ColumnSRID (so the
	// spatial index still works) and treats the data as SRID.
	DeclaredSRID bool `json:"declared_srid,omitempty"`
	ColumnSRID   int  `json:"column_srid,omitempty"`
}

// RasterSource is layers.source for a COG layer.
type RasterSource struct {
	Path     string   `json:"path"` // key inside the bucket
	Bands    int      `json:"bands"`
	DataType string   `json:"dtype"`
	NoData   *float64 `json:"nodata,omitempty"`
	SRID     int      `json:"srid,omitempty"` // 0 when the CRS has no EPSG code
	WKT      string   `json:"wkt,omitempty"`  // used when SRID is 0
	Min      *float64 `json:"min,omitempty"`  // band 1 statistics, for default scaling
	Max      *float64 `json:"max,omitempty"`
	// DeclaredSRID: SRID was set by the user instead of the file's own CRS
	// (FileSRID, 0 when it has no EPSG code).
	DeclaredSRID bool `json:"declared_srid,omitempty"`
	FileSRID     int  `json:"file_srid,omitempty"`

	// NotCOG: the file is not tiled with overviews, so MapNest serves a
	// Cloud Optimized copy it makes in its raster cache (the original is not
	// touched). Until the copy is ready, the original is served.
	NotCOG      bool   `json:"not_cog,omitempty"`
	CacheStatus string `json:"cache_status,omitempty"` // building | ready | failed
	CacheFile   string `json:"cache_file,omitempty"`   // file name in the cache dir, when ready
	CacheBytes  int64  `json:"cache_bytes,omitempty"`
	CacheError  string `json:"cache_error,omitempty"`
}

const (
	CacheBuilding = "building"
	CacheReady    = "ready"
	CacheFailed   = "failed"
)

// WithSRID sets the layer's coordinate system. 0 means "use the column's
// own SRID again".
func (s VectorSource) WithSRID(srid int) (VectorSource, error) {
	own := s.SRID
	if s.DeclaredSRID {
		own = s.ColumnSRID
	}
	switch {
	case srid == 0 && own == 0:
		return s, errors.New("the geometry column has no SRID of its own; choose an EPSG code")
	case srid == 0 || srid == own:
		s.SRID, s.DeclaredSRID, s.ColumnSRID = own, false, 0
	default:
		s.SRID, s.DeclaredSRID, s.ColumnSRID = srid, true, own
	}
	return s, nil
}

// WithSRID sets the raster's coordinate system. 0 means "use the file's
// own CRS again".
func (s RasterSource) WithSRID(srid int) RasterSource {
	own := s.SRID
	if s.DeclaredSRID {
		own = s.FileSRID
	}
	if srid == 0 || srid == own {
		s.SRID, s.DeclaredSRID, s.FileSRID = own, false, 0
	} else {
		s.SRID, s.DeclaredSRID, s.FileSRID = srid, true, own
	}
	return s
}

// Extent in the layer's native SRS.
type Extent struct {
	MinX float64 `json:"minx"`
	MinY float64 `json:"miny"`
	MaxX float64 `json:"maxx"`
	MaxY float64 `json:"maxy"`
}

// LayerTypeFor maps a PostGIS geometry type to a mapfile layer TYPE.
// Returns "" for generic types (GEOMETRY, GEOMETRYCOLLECTION).
func LayerTypeFor(geomType string) string {
	switch geomType {
	case "POINT", "MULTIPOINT", "POINTM", "MULTIPOINTM", "POINTZ", "MULTIPOINTZ":
		return "POINT"
	case "LINESTRING", "MULTILINESTRING", "LINESTRINGM", "MULTILINESTRINGM", "LINESTRINGZ", "MULTILINESTRINGZ",
		"CIRCULARSTRING", "COMPOUNDCURVE", "MULTICURVE":
		return "LINE"
	case "POLYGON", "MULTIPOLYGON", "POLYGONM", "MULTIPOLYGONM", "POLYGONZ", "MULTIPOLYGONZ",
		"CURVEPOLYGON", "MULTISURFACE", "TRIANGLE", "TIN", "POLYHEDRALSURFACE":
		return "POLYGON"
	}
	return ""
}
