package api

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"msapi/internal/store"
)

// Workspaces group stores (and so their layers) like GeoServer workspaces.
// Each one has its own WMS/WFS endpoint, /ows/<name>; the shared endpoint
// keeps serving every layer. Layer and store names stay unique overall.

// Workspace names go into a file name and a URL path, so: lowercase.
var workspaceNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

type workspaceRequest struct {
	Name        string  `json:"name"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
}

type workspaceResponse struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Stores      int    `json:"stores"`
	Layers      int    `json:"layers"`
	OWSURL      string `json:"ows_url"` // WMS and WFS endpoint of this workspace
	Default     bool   `json:"default"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func (s *Server) routeWorkspaces(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/workspaces", s.listWorkspaces)
	mux.HandleFunc("POST /api/v1/workspaces", s.createWorkspace)
	mux.HandleFunc("GET /api/v1/workspaces/{name}", s.getWorkspace)
	mux.HandleFunc("PUT /api/v1/workspaces/{name}", s.updateWorkspace)
	mux.HandleFunc("DELETE /api/v1/workspaces/{name}", s.deleteWorkspace)
}

func (s *Server) toWorkspaceResponse(w store.WorkspaceRow) workspaceResponse {
	return workspaceResponse{
		Name: w.Name, Title: w.Title, Description: w.Description, Stores: w.Stores, Layers: w.Layers,
		OWSURL: s.workspaceURL(w.Name), Default: w.Name == store.DefaultWorkspace,
		CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt,
	}
}

func (s *Server) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListWorkspaces(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	out := make([]workspaceResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.toWorkspaceResponse(row))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) findWorkspace(w http.ResponseWriter, r *http.Request) (store.WorkspaceRow, bool) {
	row, err := s.store.GetWorkspace(r.Context(), r.PathValue("name"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "workspace not found")
		return row, false
	}
	if err != nil {
		s.internalError(w, err)
		return row, false
	}
	return row, true
}

func (s *Server) getWorkspace(w http.ResponseWriter, r *http.Request) {
	if row, ok := s.findWorkspace(w, r); ok {
		writeJSON(w, http.StatusOK, s.toWorkspaceResponse(row))
	}
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (s *Server) createWorkspace(w http.ResponseWriter, r *http.Request) {
	var req workspaceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !workspaceNameRe.MatchString(req.Name) {
		writeError(w, http.StatusUnprocessableEntity,
			"name must start with a lowercase letter and contain only lowercase letters, digits and _ (max 40)")
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	row, err := s.store.CreateWorkspace(r.Context(), store.WorkspaceRow{
		Name: req.Name, Title: deref(req.Title), Description: deref(req.Description),
	})
	if errors.Is(err, store.ErrExists) {
		writeError(w, http.StatusConflict, fmt.Sprintf("workspace %q already exists", req.Name))
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	// Its (empty) endpoint exists right away.
	if err := s.rebuildLocked(r.Context(), "workspace "+row.Name+" created"); err != nil {
		if _, derr := s.store.DeleteWorkspace(r.Context(), row.Name); derr != nil {
			s.log.Error("undo workspace create", "workspace", row.Name, "error", derr)
		}
		s.writeRebuildError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.toWorkspaceResponse(row))
}

func (s *Server) updateWorkspace(w http.ResponseWriter, r *http.Request) {
	existing, ok := s.findWorkspace(w, r)
	if !ok {
		return
	}
	var req workspaceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name != "" && req.Name != existing.Name {
		writeError(w, http.StatusUnprocessableEntity, "a workspace cannot be renamed (its name is part of the service URL)")
		return
	}
	if req.Title != nil {
		existing.Title = *req.Title
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	row, err := s.store.UpdateWorkspace(r.Context(), existing)
	if err != nil {
		s.internalError(w, err)
		return
	}
	// The title is the service title in GetCapabilities.
	if err := s.rebuildLocked(r.Context(), "workspace "+row.Name+" updated"); err != nil {
		s.writeRebuildError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.toWorkspaceResponse(row))
}

func (s *Server) deleteWorkspace(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	stores, err := s.store.DeleteWorkspace(r.Context(), name)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	case errors.Is(err, store.ErrDefaultWorkspace):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		s.internalError(w, err)
		return
	case len(stores) > 0:
		writeJSON(w, http.StatusConflict, errorBody{
			Error:   fmt.Sprintf("workspace %q still has %d store(s); move or delete them first", name, len(stores)),
			Details: stores,
		})
		return
	}
	if err := s.rebuildLocked(r.Context(), "workspace "+name+" deleted"); err != nil {
		s.writeRebuildError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
