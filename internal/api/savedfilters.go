package api

import (
	"errors"
	"net/http"

	"ku-crud/internal/meta"
)

type savedFilterDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Filters   string `json:"filters"`
	CreatedAt string `json:"createdAt"`
}

func (s *Server) handleSavedFilterList(w http.ResponseWriter, r *http.Request) {
	def, _, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	u := userFrom(r)
	if !s.hasTablePerm(u, def.ID, "read") {
		writeErr(w, 403, "FORBIDDEN", "no read access to this table", nil)
		return
	}
	list, err := s.store.ListSavedFilters(u.ID, def.ID)
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	out := []savedFilterDTO{}
	for _, sf := range list {
		out = append(out, savedFilterDTO{ID: s.ids.Encode("sf", sf.ID), Name: sf.Name,
			Filters: sf.Filters, CreatedAt: sf.CreatedAt})
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleSavedFilterCreate(w http.ResponseWriter, r *http.Request) {
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	u := userFrom(r)
	if !s.hasTablePerm(u, def.ID, "read") {
		writeErr(w, 403, "FORBIDDEN", "no read access to this table", nil)
		return
	}
	var in struct {
		Name    string `json:"name"`
		Filters string `json:"filters"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	if len(in.Name) < 1 || len(in.Name) > 60 {
		writeErr(w, 400, "VALIDATION", "saved filter name must be 1..60 chars", nil)
		return
	}
	if _, fmsg := s.parseFilters(def, cols, u, in.Filters); fmsg != "" {
		writeErr(w, 400, "FILTER_INVALID", fmsg, nil)
		return
	}
	id, err := s.store.CreateSavedFilter(u.ID, def.ID, in.Name, in.Filters)
	if errors.Is(err, meta.ErrFilterTaken) {
		writeErr(w, 409, "FILTER_NAME_TAKEN", "a saved filter with this name already exists", nil)
		return
	}
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	writeJSON(w, 200, savedFilterDTO{ID: s.ids.Encode("sf", id), Name: in.Name, Filters: in.Filters})
}

// ownerFilter loads a saved filter and 404s when it is not the caller's own
// filter on this table (existence concealed across users).
func (s *Server) ownerFilter(r *http.Request, def *meta.TableDef) (*meta.SavedFilter, error) {
	id, err := s.ids.Decode("sf", r.PathValue("fid"))
	if err != nil {
		return nil, meta.ErrNotFound
	}
	sf, err := s.store.GetSavedFilter(id)
	if err != nil {
		return nil, meta.ErrNotFound
	}
	if sf.UserID != userFrom(r).ID || sf.TableDefID != def.ID {
		return nil, meta.ErrNotFound // existence concealed across users/tables
	}
	return sf, nil
}

func (s *Server) handleSavedFilterUpdate(w http.ResponseWriter, r *http.Request) {
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	u := userFrom(r)
	sf, err := s.ownerFilter(r, def)
	if err != nil {
		writeErr(w, 404, "NOT_FOUND", "saved filter not found", nil)
		return
	}
	var in struct {
		Name    string `json:"name"`
		Filters string `json:"filters"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	if len(in.Name) < 1 || len(in.Name) > 60 {
		writeErr(w, 400, "VALIDATION", "saved filter name must be 1..60 chars", nil)
		return
	}
	if _, fmsg := s.parseFilters(def, cols, u, in.Filters); fmsg != "" {
		writeErr(w, 400, "FILTER_INVALID", fmsg, nil)
		return
	}
	if err := s.store.UpdateSavedFilter(sf.ID, in.Name, in.Filters); errors.Is(err, meta.ErrFilterTaken) {
		writeErr(w, 409, "FILTER_NAME_TAKEN", "a saved filter with this name already exists", nil)
		return
	} else if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	writeJSON(w, 200, savedFilterDTO{ID: s.ids.Encode("sf", sf.ID), Name: in.Name, Filters: in.Filters})
}

func (s *Server) handleSavedFilterDelete(w http.ResponseWriter, r *http.Request) {
	def, _, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	sf, err := s.ownerFilter(r, def)
	if err != nil {
		writeErr(w, 404, "NOT_FOUND", "saved filter not found", nil)
		return
	}
	if err := s.store.DeleteSavedFilter(sf.ID); err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
