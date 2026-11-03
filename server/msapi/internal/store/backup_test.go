package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSnapshotRestore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	src, err := Open(ctx, filepath.Join(dir, "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	if _, err := src.CreateStore(ctx, StoreRow{Name: "db", Type: "postgis", Config: []byte(`{"host":"x"}`), Secret: []byte{1, 2}}); err != nil {
		t.Fatal(err)
	}
	st, _ := src.GetStore(ctx, "db")
	if _, err := src.CreateLayer(ctx, LayerRow{Name: "roads", StoreID: st.ID, Kind: "vector", Source: []byte(`{}`), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	snap := filepath.Join(dir, "snap.db")
	if err := src.Snapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}

	// A different database with other content gets fully replaced.
	dst, err := Open(ctx, filepath.Join(dir, "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	if _, err := dst.CreateStore(ctx, StoreRow{Name: "other", Type: "s3", Config: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := dst.RestoreFrom(ctx, snap); err != nil {
		t.Fatal(err)
	}
	stores, _ := dst.ListStores(ctx)
	if len(stores) != 1 || stores[0].Name != "db" || len(stores[0].Secret) != 2 {
		t.Fatalf("stores after restore: %+v", stores)
	}
	layers, _ := dst.ListLayers(ctx)
	if len(layers) != 1 || layers[0].Name != "roads" || layers[0].StoreName != "db" {
		t.Fatalf("layers after restore: %+v", layers)
	}
}
