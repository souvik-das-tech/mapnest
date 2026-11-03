package model

import "testing"

func TestVectorWithSRID(t *testing.T) {
	// Column with SRID 4326, overridden to 7755 and back.
	s := VectorSource{SRID: 4326}
	o, err := s.WithSRID(7755)
	if err != nil || o.SRID != 7755 || !o.DeclaredSRID || o.ColumnSRID != 4326 {
		t.Fatalf("override: %+v %v", o, err)
	}
	if b, _ := o.WithSRID(0); b.SRID != 4326 || b.DeclaredSRID || b.ColumnSRID != 0 {
		t.Errorf("reset: %+v", b)
	}
	if b, _ := o.WithSRID(4326); b.DeclaredSRID {
		t.Errorf("choosing the column's own SRID should clear the override: %+v", b)
	}

	// Column without SRID (QGIS import): declared 7755, changed to 32645.
	n := VectorSource{SRID: 7755, DeclaredSRID: true}
	if c, _ := n.WithSRID(32645); c.SRID != 32645 || !c.DeclaredSRID || c.ColumnSRID != 0 {
		t.Errorf("change: %+v", c)
	}
	if _, err := n.WithSRID(0); err == nil {
		t.Error("reset must fail when the column has no SRID")
	}
}

func TestRasterWithSRID(t *testing.T) {
	r := RasterSource{SRID: 32645}.WithSRID(4326)
	if r.SRID != 4326 || !r.DeclaredSRID || r.FileSRID != 32645 {
		t.Fatalf("override: %+v", r)
	}
	if b := r.WithSRID(0); b.SRID != 32645 || b.DeclaredSRID {
		t.Errorf("reset: %+v", b)
	}
}
