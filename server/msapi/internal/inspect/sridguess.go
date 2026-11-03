package inspect

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "modernc.org/sqlite" // reads PROJ's proj.db

	"msapi/internal/model"
)

// SRIDCandidate is a coordinate system the data of a column without SRID
// could be in.
type SRIDCandidate struct {
	SRID int    `json:"srid"`
	Name string `json:"name"`
	Area string `json:"area"` // area of use, e.g. "India"
	// Where the middle of the data would be with this CRS. Users know where
	// their data is, so this tells the candidates apart at a glance.
	Lon float64 `json:"lon"`
	Lat float64 `json:"lat"`
	// UsedInDatabase: other geometry columns in the same database use it,
	// the strongest hint when coordinates fit several systems.
	UsedInDatabase bool `json:"used_in_database"`
}

// crsInfo is one projected EPSG CRS from proj.db: area of use (degrees) and,
// when known, its false origin, for a cheap first estimate.
type crsInfo struct {
	code                     int
	name, area               string
	method                   string // e.g. "EPSG:9807" (Transverse Mercator)
	west, south, east, north float64
	hasOrigin                bool
	lon0, lat0               float64 // degrees
	fe, fn                   float64 // in the CRS's linear unit
	unit                     float64 // metres per linear unit
}

var (
	crsOnce sync.Once
	crsList []crsInfo
	crsErr  error
)

// EPSG parameter codes for the origin of common projection methods.
var (
	lonParams = map[int]bool{8802: true, 8822: true, 8812: true} // natural origin, false origin, projection centre
	latParams = map[int]bool{8801: true, 8821: true, 8811: true}
	feParams  = map[int]bool{8806: true, 8826: true, 8816: true}
	fnParams  = map[int]bool{8807: true, 8827: true, 8817: true}
)

// loadCRS reads every non-deprecated projected EPSG CRS from proj.db (shipped
// with PROJ in the msapi image), once.
func loadCRS(projDB string) ([]crsInfo, error) {
	crsOnce.Do(func() { crsList, crsErr = readProjDB(projDB) })
	return crsList, crsErr
}

func readProjDB(projDB string) ([]crsInfo, error) {
	db, err := sql.Open("sqlite", "file:"+projDB+"?mode=ro&immutable=1")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Unit factors: metres per linear unit, radians per angular unit.
	factors := map[string]float64{}
	urows, err := db.Query(`SELECT auth_name || ':' || code, COALESCE(conv_factor, 0) FROM unit_of_measure`)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", projDB, err)
	}
	for urows.Next() {
		var k string
		var f float64
		if err := urows.Scan(&k, &f); err != nil {
			urows.Close()
			return nil, err
		}
		factors[k] = f
	}
	urows.Close()

	cols := []string{"p.code", "p.name", "e.name", "e.west_lon", "e.south_lat", "e.east_lon", "e.north_lat",
		"COALESCE(c.method_auth_name || ':' || c.method_code, '')"}
	for i := 1; i <= 7; i++ {
		cols = append(cols, fmt.Sprintf("c.param%d_code", i), fmt.Sprintf("c.param%d_value", i),
			fmt.Sprintf("c.param%d_uom_auth_name || ':' || c.param%d_uom_code", i, i))
	}
	// A CRS can have several usages; the widest area of use is read first.
	rows, err := db.Query(`SELECT ` + strings.Join(cols, ", ") + `
		FROM projected_crs p
		JOIN usage u ON u.object_table_name = 'projected_crs' AND u.object_auth_name = p.auth_name AND u.object_code = p.code
		JOIN extent e ON e.auth_name = u.extent_auth_name AND e.code = u.extent_code
		LEFT JOIN conversion_table c ON c.auth_name = p.conversion_auth_name AND c.code = p.conversion_code
		WHERE p.auth_name = 'EPSG' AND p.deprecated = 0 AND e.west_lon < e.east_lon
		ORDER BY (e.east_lon - e.west_lon) * (e.north_lat - e.south_lat) DESC`)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", projDB, err)
	}
	defer rows.Close()
	seen := map[int]bool{}
	var out []crsInfo
	for rows.Next() {
		var c crsInfo
		var code string
		var pcode [7]sql.NullInt64
		var pval [7]sql.NullFloat64
		var puom [7]sql.NullString
		dest := []any{&code, &c.name, &c.area, &c.west, &c.south, &c.east, &c.north, &c.method}
		for i := range 7 {
			dest = append(dest, &pcode[i], &pval[i], &puom[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		if _, err := fmt.Sscan(code, &c.code); err != nil || seen[c.code] {
			continue
		}
		seen[c.code] = true
		var gotLon, gotLat, gotFE, gotFN bool
		for i := range 7 {
			if !pcode[i].Valid || !pval[i].Valid {
				continue
			}
			p, v, uom := int(pcode[i].Int64), pval[i].Float64, puom[i].String
			switch {
			case lonParams[p]:
				c.lon0, gotLon = angleDeg(v, uom, factors), true
			case latParams[p]:
				c.lat0, gotLat = angleDeg(v, uom, factors), true
			case feParams[p]:
				c.fe, gotFE = v, true
				c.unit = factors[uom]
			case fnParams[p]:
				c.fn, gotFN = v, true
			}
		}
		c.hasOrigin = gotLon && gotLat && gotFE && gotFN && c.unit > 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// angleDeg converts an angle to degrees. EPSG:9110 is sexagesimal DMS
// written as DDD.MMSSsss.
func angleDeg(v float64, uom string, factors map[string]float64) float64 {
	if uom == "EPSG:9110" {
		sign := 1.0
		if v < 0 {
			sign, v = -1, -v
		}
		d := math.Floor(v)
		rest := (v - d) * 100
		m := math.Floor(rest + 1e-9)
		s := (rest - m) * 100
		return sign * (d + m/60 + s/3600)
	}
	if f := factors[uom]; f > 0 {
		return v * f * 180 / math.Pi
	}
	return v
}

// roughBox estimates the CRS's area of use in its own units, treating the
// projection as locally equidistant around its origin. Good to a few tens
// of percent for the usual national/zone systems; used only to skip
// impossible candidates before the exact check.
func (c crsInfo) roughBox() (minx, miny, maxx, maxy float64) {
	minx, miny = math.Inf(1), math.Inf(1)
	maxx, maxy = math.Inf(-1), math.Inf(-1)
	for _, lat := range []float64{c.south, (c.south + c.north) / 2, c.north} {
		for _, lon := range []float64{c.west, (c.west + c.east) / 2, c.east, c.lon0} {
			if lon < c.west || lon > c.east {
				continue
			}
			x := c.fe + (lon-c.lon0)*111320*math.Cos(lat*math.Pi/180)/c.unit
			y := c.fn + (lat-c.lat0)*110574/c.unit
			minx, maxx = math.Min(minx, x), math.Max(maxx, x)
			miny, maxy = math.Min(miny, y), math.Max(maxy, y)
		}
	}
	return
}

// GuessSRID suggests coordinate systems for a geometry column without SRID,
// best first. Coordinates within ±180/±90 are taken as longitude/latitude.
// Otherwise a projected EPSG CRS is a candidate when its area of use,
// projected, contains the whole data extent, and the middle of the data
// maps back into that area of use. The projection math runs in the user's
// PostGIS (read-only ST_Transform on constants).
//
// Coordinates alone can fit many systems (any UTM zone fits UTM-style
// numbers), so each candidate says where the data would be, and the order
// uses hints: systems other tables in the database use, a plausible
// latitude, and closeness to hintLon (e.g. from the browser's time zone).
func GuessSRID(ctx context.Context, pool *pgxpool.Pool, projDB string, req VectorRequest, hintLon *float64) ([]SRIDCandidate, *model.Extent, error) {
	src := model.VectorSource{Schema: req.Schema, Table: req.Table, GeomColumn: req.GeomColumn}
	if src.GeomColumn == "" {
		err := pool.QueryRow(ctx, `SELECT f_geometry_column FROM geometry_columns
			WHERE f_table_schema = $1 AND f_table_name = $2 ORDER BY f_geometry_column LIMIT 1`,
			req.Schema, req.Table).Scan(&src.GeomColumn)
		if err != nil {
			return nil, nil, fmt.Errorf("%s.%s has no geometry column", req.Schema, req.Table)
		}
	}
	ext, err := extent(ctx, pool, src)
	if err != nil || ext == nil {
		return nil, ext, err
	}
	cx, cy := (ext.MinX+ext.MaxX)/2, (ext.MinY+ext.MaxY)/2
	if ext.MinX >= -180 && ext.MaxX <= 180 && ext.MinY >= -90 && ext.MaxY <= 90 {
		return []SRIDCandidate{{SRID: 4326, Name: "WGS 84", Area: "World (longitude/latitude)", Lon: cx, Lat: cy}}, ext, nil
	}

	all, err := loadCRS(projDB)
	if err != nil {
		return nil, ext, err
	}
	// First pass without PROJ: the data must fit the rough projected area of
	// use, with a wide margin since the estimate ignores the projection's
	// curvature. Systems without a known origin always go to the exact check.
	var pre []crsInfo
	for _, c := range all {
		if !c.hasOrigin {
			pre = append(pre, c)
			continue
		}
		minx, miny, maxx, maxy := c.roughBox()
		mx := (maxx-minx)*0.5 + 100000/c.unit
		my := (maxy-miny)*0.5 + 100000/c.unit
		if ext.MinX >= minx-mx && ext.MaxX <= maxx+mx && ext.MinY >= miny-my && ext.MaxY <= maxy+my {
			pre = append(pre, c)
		}
	}

	used := map[int]bool{}
	if rows, err := pool.Query(ctx, `SELECT DISTINCT srid FROM geometry_columns WHERE srid > 0`); err == nil {
		ids, _ := pgx.CollectRows(rows, pgx.RowTo[int32])
		for _, id := range ids {
			used[int(id)] = true
		}
	}

	// Systems with the same method and origin (e.g. UTM 45N on different
	// datums) differ only by a datum shift of up to a few hundred metres, so
	// one representative per group is checked exactly, over the union of the
	// group's areas of use. Each member then needs the result inside its own
	// area of use. This cuts the PostGIS work several times.
	type group struct {
		rep     crsInfo
		members []crsInfo
	}
	groups := map[string]*group{}
	var order []string
	for _, c := range pre {
		key := fmt.Sprint("code:", c.code)
		if c.hasOrigin {
			key = fmt.Sprintf("%s|%.6f|%.6f|%.3f|%.3f|%.9f", c.method, c.lon0, c.lat0, c.fe, c.fn, c.unit)
		}
		g, ok := groups[key]
		if !ok {
			g = &group{rep: c}
			groups[key] = g
			order = append(order, key)
		}
		g.rep.west, g.rep.south = math.Min(g.rep.west, c.west), math.Min(g.rep.south, c.south)
		g.rep.east, g.rep.north = math.Max(g.rep.east, c.east), math.Max(g.rep.north, c.north)
		g.members = append(g.members, c)
	}
	reps := make([]crsInfo, len(order))
	for i, k := range order {
		reps[i] = groups[k].rep
	}
	checked, err := exactCheck(ctx, pool, reps, ext)
	if err != nil {
		return nil, ext, err
	}
	type hit struct {
		c        crsInfo
		lon, lat float64
	}
	var hits []hit
	for _, k := range order {
		g := groups[k]
		p, ok := checked[g.rep.code]
		if !ok {
			continue
		}
		for _, c := range g.members {
			if p[0] >= c.west-1 && p[0] <= c.east+1 && p[1] >= c.south-1 && p[1] <= c.north+1 {
				hits = append(hits, hit{c, p[0], p[1]})
			}
		}
	}

	isWGS84 := func(c crsInfo) bool { return strings.HasPrefix(c.name, "WGS 84") }
	// Distance from the hint longitude in 15° steps (one time zone), so the
	// finer preferences below still apply between nearby candidates.
	near := func(lon float64) int {
		if hintLon == nil {
			return 0
		}
		d := math.Abs(math.Mod(lon-*hintLon+540, 360) - 180)
		return int(d / 15)
	}
	polar := func(lat float64) bool { return math.Abs(lat) > 72 || lat < -56 } // ice caps, Southern Ocean
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if used[a.c.code] != used[b.c.code] {
			return used[a.c.code]
		}
		if polar(a.lat) != polar(b.lat) {
			return !polar(a.lat)
		}
		if na, nb := near(a.lon), near(b.lon); na != nb {
			return na < nb
		}
		if isWGS84(a.c) != isWGS84(b.c) {
			return isWGS84(a.c) // most data made today is on WGS 84
		}
		aa := (a.c.east - a.c.west) * (a.c.north - a.c.south)
		ab := (b.c.east - b.c.west) * (b.c.north - b.c.south)
		if aa != ab {
			return aa < ab // most specific area of use first
		}
		return a.c.code < b.c.code
	})
	out := []SRIDCandidate{}
	for _, h := range hits {
		out = append(out, SRIDCandidate{SRID: h.c.code, Name: h.c.name, Area: h.c.area, Lon: h.lon, Lat: h.lat, UsedInDatabase: used[h.c.code]})
		if len(out) == 100 {
			break
		}
	}
	return out, ext, nil
}

// exactCheck projects each candidate's area of use with PostGIS and returns,
// for those whose projected area contains the data extent and whose inverse
// of the data's middle falls inside the area of use, that middle as lon/lat.
// Codes the database does not know or cannot transform are left out. A
// batch that fails is split until the bad code is isolated.
func exactCheck(ctx context.Context, pool *pgxpool.Pool, cands []crsInfo, ext *model.Extent) (map[int][2]float64, error) {
	out := map[int][2]float64{}
	if len(cands) == 0 {
		return out, nil
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	cx, cy := (ext.MinX+ext.MaxX)/2, (ext.MinY+ext.MaxY)/2

	var run func(batch []crsInfo) error
	run = func(batch []crsInfo) error {
		if len(batch) == 0 {
			return nil
		}
		codes := make([]int32, len(batch))
		w, s, e, n := make([]float64, len(batch)), make([]float64, len(batch)), make([]float64, len(batch)), make([]float64, len(batch))
		for i, c := range batch {
			codes[i], w[i], s[i], e[i], n[i] = int32(c.code), c.west, c.south, c.east, c.north
		}
		if _, err := tx.Exec(ctx, `SAVEPOINT guess`); err != nil {
			return err
		}
		// Densify the lon/lat box so curved edges are followed. The middle of
		// the data must fall in the area of use (±1°); the edges may reach 15%
		// past it, since areas of use are approximate and boundary data often
		// covers a bit more (e.g. India's official extent goes beyond 35.5°N).
		rows, err := tx.Query(ctx, `
			SELECT c.srid, ST_X(p), ST_Y(p)
			FROM unnest($1::int[], $2::float8[], $3::float8[], $4::float8[], $5::float8[]) AS c(srid, w, s, e, n)
			JOIN spatial_ref_sys r ON r.srid = c.srid
			CROSS JOIN LATERAL (
				SELECT ST_Transform(ST_Segmentize(ST_MakeEnvelope(c.w, c.s, c.e, c.n, 4326), 0.5), c.srid)::box2d AS b
			) t
			CROSS JOIN LATERAL (
				SELECT ST_Transform(ST_SetSRID(ST_MakePoint($10, $11), c.srid), 4326) AS p
			) m
			WHERE $6 >= ST_XMin(b) - (ST_XMax(b) - ST_XMin(b)) * 0.15 AND $8 <= ST_XMax(b) + (ST_XMax(b) - ST_XMin(b)) * 0.15
			  AND $7 >= ST_YMin(b) - (ST_YMax(b) - ST_YMin(b)) * 0.15 AND $9 <= ST_YMax(b) + (ST_YMax(b) - ST_YMin(b)) * 0.15
			  AND ST_X(p) BETWEEN c.w - 1 AND c.e + 1 AND ST_Y(p) BETWEEN c.s - 1 AND c.n + 1`,
			codes, w, s, e, n, ext.MinX, ext.MinY, ext.MaxX, ext.MaxY, cx, cy)
		got := map[int][2]float64{}
		if err == nil {
			for rows.Next() {
				var code int32
				var lon, lat float64
				if err = rows.Scan(&code, &lon, &lat); err != nil {
					break
				}
				got[int(code)] = [2]float64{lon, lat}
			}
			rows.Close()
			if err == nil {
				err = rows.Err()
			}
		}
		if err == nil {
			_, err = tx.Exec(ctx, `RELEASE SAVEPOINT guess`)
			for k, v := range got {
				out[k] = v
			}
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, rerr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT guess`); rerr != nil {
			return rerr
		}
		if len(batch) == 1 {
			return nil // this CRS cannot be transformed here; skip it
		}
		mid := len(batch) / 2
		if err := run(batch[:mid]); err != nil {
			return err
		}
		return run(batch[mid:])
	}
	for i := 0; i < len(cands); i += 200 {
		if err := run(cands[i:min(i+200, len(cands))]); err != nil {
			return nil, err
		}
	}
	return out, nil
}
