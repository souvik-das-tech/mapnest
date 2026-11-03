package mapfile

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"msapi/internal/model"
)

func TestBuild(t *testing.T) {
	minScale := 1000.0
	m := Map{
		PublicURL: "http://example.org/",
		Config:    []KV{{"AWS_S3_ENDPOINT", "s3:8333"}},
		Layers: []Layer{
			{
				Name: "cities", Title: `Big "cities"`, Type: "POINT",
				Connection: "service=city_db",
				Data:       VectorData(model.VectorSource{Schema: "public", Table: "cities", GeomColumn: "geom", PK: "id", SRID: 4326}),
				Projection: "EPSG:4326", SRS: layerSRS4326, PK: "id",
				Extent:   &model.Extent{MinX: 72, MinY: 12, MaxX: 89, MaxY: 29},
				MinScale: &minScale,
				Classes:  DefaultClasses("POINT"),
			},
			{
				Name: "dem", Type: "RASTER", Data: "/vsis3/rasters/dem.tif",
				Projection: "AUTO", Processing: []string{"SCALE=0,60"},
			},
		},
	}
	out, err := Build(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`CONFIG "AWS_S3_ENDPOINT" "s3:8333"`,
		`NAME "cities"`,
		`CONNECTION "service=city_db"`,
		`DATA "geom FROM \"public\".\"cities\" USING UNIQUE id USING SRID=4326"`,
		`"ows_title"          "Big \"cities\""`,
		`MINSCALEDENOM 1000`,
		`EXTENT 72 12 89 29`,
		`"wfs_enable_request" "!*"`,
		`SYMBOL "circle"`,
		`PROCESSING "SCALE=0,60"`,
		"PROJECTION\n      AUTO\n    END",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out[strings.Index(out, `NAME "dem"`):], "TEMPLATE") {
		t.Error("raster layer must not get a query TEMPLATE")
	}
}

const layerSRS4326 = "EPSG:4326 EPSG:3857 EPSG:900913"

func TestQuote(t *testing.T) {
	if got := Quote("a\"b\\c\nd"); got != `"a\"b\\c d"` {
		t.Errorf("Quote = %s", got)
	}
}

func TestApplyKeepsLiveFileOnRejection(t *testing.T) {
	dir := t.TempDir()
	ok := func(context.Context, string) error { return nil }
	reject := func(context.Context, string) error { return &RejectedError{Msg: "bad"} }

	if err := Apply(context.Background(), dir, "MAP END\n", ok); err != nil {
		t.Fatal(err)
	}
	err := Apply(context.Background(), dir, "broken", reject)
	var re *RejectedError
	if !errors.As(err, &re) {
		t.Fatalf("err = %v, want RejectedError", err)
	}
	got, _ := os.ReadFile(MapfilePath(dir))
	if string(got) != "MAP END\n" {
		t.Errorf("live mapfile changed to %q", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestApplySet(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	ok := func(context.Context, string) error { return nil }
	set := []File{{MapfileName, "all"}, {WorkspaceFile("a"), "a"}, {WorkspaceFile("b"), "b"}}
	if err := ApplySet(ctx, dir, set, ok); err != nil {
		t.Fatal(err)
	}

	// One bad file: nothing changes.
	rejectB := func(_ context.Context, p string) error {
		if b, _ := os.ReadFile(p); string(b) == "b2" {
			return &RejectedError{Msg: "bad"}
		}
		return nil
	}
	err := ApplySet(ctx, dir, []File{{MapfileName, "all2"}, {WorkspaceFile("b"), "b2"}}, rejectB)
	var re *RejectedError
	if !errors.As(err, &re) || !strings.Contains(re.Msg, "ws_b.map") {
		t.Fatalf("err = %v, want RejectedError naming ws_b.map", err)
	}
	if got, _ := os.ReadFile(MapfilePath(dir)); string(got) != "all" {
		t.Errorf("services.map changed to %q", got)
	}

	// Workspace a is gone: its mapfile is removed.
	if err := ApplySet(ctx, dir, []File{{MapfileName, "all3"}, {WorkspaceFile("b"), "b3"}}, ok); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "services.map,ws_b.map" {
		t.Errorf("files = %v", names)
	}
}

func TestVectorDataDeclaredSRID(t *testing.T) {
	src := model.VectorSource{Schema: "public", Table: "zones", GeomColumn: "geom", PK: "id", SRID: 4326}
	if got := VectorData(src); !strings.HasSuffix(got, "USING SRID=4326") {
		t.Errorf("column SRID: %s", got)
	}
	// A column without SRID is queried with SRID 0; the declared CRS goes in PROJECTION.
	src.DeclaredSRID = true
	if got := VectorData(src); !strings.HasSuffix(got, "USING SRID=0") {
		t.Errorf("declared SRID: %s", got)
	}
}

func TestExpandLayersSimplified(t *testing.T) {
	base := Layer{Name: "districts", Type: "POLYGON", Connection: "service=db", Data: "full", MapData: "simple",
		GeomColumn: "geom", PK: "id", Projection: "EPSG:4326", Classes: "CLASS\nEND"}

	one := expandLayers([]Layer{base})
	if len(one) != 1 || one[0].Data != "simple" || one[0].Name != "districts" {
		t.Fatalf("without WFS: %+v", one)
	}

	base.WFS = true
	two := expandLayers([]Layer{base})
	if len(two) != 2 {
		t.Fatalf("with WFS: %d layers", len(two))
	}
	draw, feat := two[0], two[1]
	if draw.Name != "districts__map" || draw.Group != "districts" || draw.Data != "simple" || draw.WFS {
		t.Errorf("drawing layer: %+v", draw)
	}
	if feat.Name != "districts" || !feat.WMSOff || feat.Data != "full" || !feat.WFS || feat.Classes != "" {
		t.Errorf("WFS layer: %+v", feat)
	}
	out, err := Build(Map{Layers: []Layer{base}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`NAME "districts__map"`, `GROUP "districts"`, `"wms_enable_request" "!*"`, `"gml_exclude_items"  "geom"`} {
		if !strings.Contains(out, want) {
			t.Errorf("mapfile lacks %s", want)
		}
	}
}

func TestVectorMapData(t *testing.T) {
	src := model.VectorSource{Schema: "public", Table: "d", GeomColumn: "geom", PK: "id", SRID: 7755, DeclaredSRID: true}
	got := VectorMapData(src)
	for _, want := range []string{"mapnest_geom FROM (SELECT *, ST_Simplify(\"geom\"", `"geom" && !BOX!`, "USING UNIQUE id USING SRID=0"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s lacks %s", got, want)
		}
	}
}
