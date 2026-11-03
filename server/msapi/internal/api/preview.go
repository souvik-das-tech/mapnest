package api

import (
	"context"
	"net/url"

	"msapi/internal/mapfile"
	"msapi/internal/store"
)

// renderPreview renders one layer with the given CLASS blocks without
// touching the live mapfile.
func (s *Server) renderPreview(ctx context.Context, l store.LayerRow, classes string, opacity *float64, params url.Values) ([]byte, error) {
	m, err := s.buildMap(ctx, buildOpts{only: l.Name, classes: &classes, opacity: opacity})
	if err != nil {
		return nil, err
	}
	content, err := mapfile.Build(m)
	if err != nil {
		return nil, err
	}
	return mapfile.RenderMapserv(ctx, s.cfg.ValidateConfig, s.cfg.MapfileDir, content, params)
}
