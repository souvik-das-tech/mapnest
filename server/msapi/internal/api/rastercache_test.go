package api

import (
	"encoding/json"
	"testing"

	"msapi/internal/model"
)

func TestKeepRasterCache(t *testing.T) {
	raw := func(r model.RasterSource) json.RawMessage { b, _ := json.Marshal(r); return b }
	old := raw(model.RasterSource{Path: "a.tif", NotCOG: true, CacheStatus: model.CacheReady, CacheFile: "a-1.tif", CacheBytes: 10})

	// Same non-COG file: the old copy keeps being served while a new one is made.
	var got model.RasterSource
	json.Unmarshal(keepRasterCache(old, raw(model.RasterSource{Path: "a.tif", NotCOG: true})), &got)
	if got.CacheFile != "a-1.tif" || got.CacheStatus != model.CacheBuilding {
		t.Errorf("same file: %+v", got)
	}
	// The file became Cloud Optimized: no copy.
	got = model.RasterSource{}
	json.Unmarshal(keepRasterCache(old, raw(model.RasterSource{Path: "a.tif"})), &got)
	if got.CacheFile != "" {
		t.Errorf("now COG: %+v", got)
	}
	if !needsRasterCache(raw(model.RasterSource{NotCOG: true})) || needsRasterCache(raw(model.RasterSource{})) {
		t.Error("needsRasterCache")
	}
}
