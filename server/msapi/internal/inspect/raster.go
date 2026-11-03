package inspect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"msapi/internal/model"
)

type RasterResult struct {
	Source   model.RasterSource
	Extent   model.Extent
	Width    int
	Height   int
	Warnings []string
}

// gdalinfo -json output, only the fields we use.
type gdalInfo struct {
	Size     []int `json:"size"`
	Metadata struct {
		ImageStructure map[string]string `json:"IMAGE_STRUCTURE"`
	} `json:"metadata"`
	CoordinateSystem *struct {
		WKT string `json:"wkt"`
	} `json:"coordinateSystem"`
	Stac struct {
		EPSG *int `json:"proj:epsg"`
	} `json:"stac"`
	CornerCoordinates map[string][]float64 `json:"cornerCoordinates"`
	Bands             []struct {
		Band        int      `json:"band"`
		Block       []int    `json:"block"`
		Type        string   `json:"type"`
		NoDataValue *gdalNum `json:"noDataValue"`
		Minimum     *float64 `json:"minimum"`
		Maximum     *float64 `json:"maximum"`
		Overviews   []any    `json:"overviews"`
		ColorInterp string   `json:"colorInterpretation"`
	} `json:"bands"`
}

// gdalNum accepts numbers and the strings "nan"/"inf" GDAL may emit.
type gdalNum struct{ V float64 }

func (n *gdalNum) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	switch strings.ToLower(s) {
	case "nan":
		n.V = math.NaN()
		return nil
	case "inf", "infinity":
		n.V = math.Inf(1)
		return nil
	case "-inf", "-infinity":
		n.V = math.Inf(-1)
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	n.V = v
	return err
}

// ValidRasterPath checks an object key (or a file path relative to a local
// store folder) before it is used in a /vsis3/ path or the mapfile.
func ValidRasterPath(p string) error {
	lower := strings.ToLower(p)
	switch {
	case p == "" || strings.HasPrefix(p, "/"):
		return errors.New("path must be relative to the bucket or folder, without a leading /")
	case strings.Contains(p, ".."), strings.ContainsAny(p, "\"'\r\n\\"):
		return errors.New("path contains characters that are not allowed")
	case !strings.HasSuffix(lower, ".tif") && !strings.HasSuffix(lower, ".tiff"):
		return errors.New("path must end with .tif or .tiff")
	}
	return nil
}

// GDALEnv returns the environment for GDAL to read from the store's bucket.
func GDALEnv(c model.S3Conn) []string {
	env := []string{
		"AWS_ACCESS_KEY_ID=" + c.AccessKey,
		"AWS_SECRET_ACCESS_KEY=" + c.SecretKey,
		"AWS_REGION=" + c.Region,
		"AWS_HTTPS=" + map[bool]string{true: "YES", false: "NO"}[c.UseHTTPS],
		"AWS_VIRTUAL_HOSTING=" + map[bool]string{true: "TRUE", false: "FALSE"}[c.VirtualHosting],
		"GDAL_DISABLE_READDIR_ON_OPEN=EMPTY_DIR",
		"CPL_VSIL_CURL_ALLOWED_EXTENSIONS=.tif,.tiff",
	}
	if c.Endpoint != "" {
		env = append(env, "AWS_S3_ENDPOINT="+c.Endpoint)
	}
	return env
}

// RasterInput says where gdalinfo reads a raster from.
type RasterInput struct {
	Path  string   // as the user gave it; stored in RasterSource.Path
	Data  string   // what GDAL opens: /vsis3/... or an absolute file path
	Env   []string // extra environment for gdalinfo
	Local bool     // local file (affects only the wording of speed warnings)
	// SRID declares the CRS of a file without one; ignored when it has one.
	SRID int
}

// S3Input reads <path> from the store's bucket through /vsis3/.
func S3Input(c model.S3Conn, path string) RasterInput {
	return RasterInput{Path: path, Data: "/vsis3/" + c.Bucket + "/" + path, Env: GDALEnv(c)}
}

// InspectRaster runs gdalinfo -json on the raster. Any GeoTIFF GDAL reads
// is accepted, like in GeoServer; one that is not cloud-optimized (tiled
// with overviews) gets a warning, since large ones render slowly, and on S3
// a zoomed-out map then downloads most of the file.
func InspectRaster(ctx context.Context, in RasterInput) (RasterResult, error) {
	var res RasterResult
	path := in.Path
	if err := ValidRasterPath(path); err != nil {
		return res, err
	}
	vsi := in.Data

	info, err := gdalinfo(ctx, in.Env, vsi, false)
	if err != nil {
		return res, err
	}
	if len(info.Size) != 2 || len(info.Bands) == 0 {
		return res, errors.New("gdalinfo returned no size or bands")
	}
	res.Width, res.Height = info.Size[0], info.Size[1]
	b1 := info.Bands[0]

	// Cloud-optimized check: COG layout, or at least tiled with overviews.
	tiled := len(b1.Block) == 2 && b1.Block[0] < res.Width
	small := res.Width <= 2048 && res.Height <= 2048
	isCOG := strings.EqualFold(info.Metadata.ImageStructure["LAYOUT"], "COG")
	notCOG := !isCOG && (!tiled || (len(b1.Overviews) == 0 && !small))
	if notCOG {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"%s is not a Cloud Optimized GeoTIFF (not tiled with overviews), so MapNest makes a fast copy of it in the background; the original is not changed. Until the copy is ready, zoomed-out maps of this %d×%d raster are slow.",
			path, res.Width, res.Height))
	} else if !isCOG && !in.Local {
		res.Warnings = append(res.Warnings, "file is tiled with overviews but not marked LAYOUT=COG")
	}

	src := model.RasterSource{Path: path, Bands: len(info.Bands), DataType: b1.Type, NotCOG: notCOG}
	if b1.NoDataValue != nil && !math.IsNaN(b1.NoDataValue.V) && !math.IsInf(b1.NoDataValue.V, 0) {
		v := b1.NoDataValue.V
		src.NoData = &v
	}

	// CRS.
	if info.CoordinateSystem == nil || info.CoordinateSystem.WKT == "" {
		if in.SRID <= 0 {
			return res, errors.New("raster has no coordinate reference system; set srid to declare it (e.g. 4326), like GeoServer's Declared SRS")
		}
		src.SRID, src.DeclaredSRID = in.SRID, true
	} else if info.Stac.EPSG != nil {
		src.SRID = *info.Stac.EPSG
	} else {
		src.SRID = epsgFromWKT(info.CoordinateSystem.WKT)
	}
	if src.SRID == 0 && info.CoordinateSystem != nil {
		src.WKT = info.CoordinateSystem.WKT
		res.Warnings = append(res.Warnings, "CRS has no EPSG code; MapServer will read it from the file (PROJECTION AUTO)")
	}

	// Extent from the corner coordinates (native CRS).
	ext, err := cornerExtent(info.CornerCoordinates)
	if err != nil {
		return res, err
	}
	res.Extent = ext

	// Single non-8-bit band (e.g. a DEM): approximate statistics from the
	// overviews, used for the default stretch.
	if src.Bands == 1 && src.DataType != "Byte" {
		if stats, err := gdalinfo(ctx, in.Env, vsi, true); err == nil && len(stats.Bands) > 0 {
			src.Min, src.Max = stats.Bands[0].Minimum, stats.Bands[0].Maximum
		} else {
			res.Warnings = append(res.Warnings, "could not compute statistics; default style uses SCALE=AUTO")
		}
	}
	res.Source = src
	return res, nil
}

func gdalinfo(ctx context.Context, env []string, vsi string, stats bool) (gdalInfo, error) {
	var info gdalInfo
	timeout := 30 * time.Second
	args := []string{"-json"}
	if stats {
		args = []string{"-json", "-approx_stats"}
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gdalinfo", append(args, vsi)...)
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return info, errors.New("gdalinfo not found; msapi must run in its Docker image (MapServer base)")
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return info, fmt.Errorf("gdalinfo %s: %s", vsi, msg)
	}
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return info, fmt.Errorf("parse gdalinfo output: %w", err)
	}
	return info, nil
}

var epsgRe = regexp.MustCompile(`ID\["EPSG",(\d+)\]\s*\]\s*$`)

// epsgFromWKT returns the top-level EPSG ID of a WKT2 string, or 0.
func epsgFromWKT(wkt string) int {
	if m := epsgRe.FindStringSubmatch(strings.TrimSpace(wkt)); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

func cornerExtent(cc map[string][]float64) (model.Extent, error) {
	var e model.Extent
	first := true
	for _, k := range []string{"upperLeft", "lowerLeft", "upperRight", "lowerRight"} {
		p, ok := cc[k]
		if !ok || len(p) < 2 {
			return e, errors.New("gdalinfo returned no corner coordinates")
		}
		if first {
			e = model.Extent{MinX: p[0], MinY: p[1], MaxX: p[0], MaxY: p[1]}
			first = false
			continue
		}
		e.MinX, e.MaxX = math.Min(e.MinX, p[0]), math.Max(e.MaxX, p[0])
		e.MinY, e.MaxY = math.Min(e.MinY, p[1]), math.Max(e.MaxY, p[1])
	}
	return e, nil
}
