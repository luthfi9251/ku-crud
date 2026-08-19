package api

import (
	"errors"
	"net/http"
	"strings"

	"ku-crud/internal/meta"
)

type grantDTO struct {
	TableDefID string `json:"tableDefId"`
	CanRead    bool   `json:"canRead"`
	CanCreate  bool   `json:"canCreate"`
	CanUpdate  bool   `json:"canUpdate"`
	CanDelete  bool   `json:"canDelete"`
}

type roleDTO struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	IsAdmin        bool       `json:"isAdmin"`
	PlatformManage bool       `json:"platformManage"`
	Grants         []grantDTO `json:"tables"`
	UserCount      int        `json:"userCount"`
}

type roleInput struct {
	Name           string     `json:"name"`
	PlatformManage bool       `json:"platformManage"`
	Tables         []grantDTO `json:"tables"`
}

// toGrants decodes masked table-def tokens; every referenced table must exist.
func (s *Server) toGrants(in []grantDTO) ([]meta.TableGrant, error) {
	out := make([]meta.TableGrant, 0, len(in))
	seen := map[int64]bool{}
	for _, g := range in {
		id, err := s.ids.Decode("td", g.TableDefID)
		if err != nil {
			return nil, errors.New("invalid tableDefId in grants")
		}
		if seen[id] {
			continue // dedupe
		}
		if _, _, err := s.store.GetTableDef(id); err != nil {
			return nil, errors.New("unknown table in grants")
		}
		seen[id] = true
		out = append(out, meta.TableGrant{TableDefID: id,
			CanRead: g.CanRead, CanCreate: g.CanCreate, CanUpdate: g.CanUpdate, CanDelete: g.CanDelete})
	}
	return out, nil
}

func (s *Server) toRoleDTO(r meta.Role, grants []meta.TableGrant, userCount int) roleDTO {
	dto := roleDTO{
		ID: s.ids.Encode("role", r.ID), Name: r.Name,
		IsAdmin: r.IsAdmin, PlatformManage: r.PlatformManage,
		Grants: make([]grantDTO, 0, len(grants)), UserCount: userCount,
	}
	for _, g := range grants {
		dto.Grants = append(dto.Grants, grantDTO{
			TableDefID: s.ids.Encode("td", g.TableDefID),
			CanRead:    g.CanRead, CanCreate: g.CanCreate,
			CanUpdate: g.CanUpdate, CanDelete: g.CanDelete,
		})
	}
	return dto
}

func (s *Server) handleRoleList(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListRoles()
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	out := make([]roleDTO, len(list))
	for i, rl := range list {
		out[i] = s.toRoleDTO(rl.Role, rl.Grants, rl.UserCount)
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleRoleCreate(w http.ResponseWriter, r *http.Request) {
	var in roleInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 64 {
		writeErr(w, 400, "VALIDATION", "role name is required (max 64 chars)", nil)
		return
	}
	grants, err := s.toGrants(in.Tables)
	if err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	role := &meta.Role{Name: in.Name, PlatformManage: in.PlatformManage}
	if err := s.store.CreateRole(role, grants); err != nil {
		writeErr(w, 400, "VALIDATION", "could not create role (duplicate name?)", err.Error())
		return
	}
	writeJSON(w, 200, s.toRoleDTO(*role, grants, 0))
}

func (s *Server) handleRoleUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := s.ids.Decode("role", r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "NOT_FOUND", "role not found", nil)
		return
	}
	var in roleInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 64 {
		writeErr(w, 400, "VALIDATION", "role name is required (max 64 chars)", nil)
		return
	}
	grants, err := s.toGrants(in.Tables)
	if err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	role := &meta.Role{ID: id, Name: in.Name, PlatformManage: in.PlatformManage}
	if err := s.store.UpdateRole(role, grants); err != nil {
		s.writeRoleErr(w, err)
		return
	}
	writeJSON(w, 200, s.toRoleDTO(*role, grants, 0))
}

func (s *Server) handleRoleDelete(w http.ResponseWriter, r *http.Request) {
	id, err := s.ids.Decode("role", r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "NOT_FOUND", "role not found", nil)
		return
	}
	if err := s.store.DeleteRole(id); err != nil {
		s.writeRoleErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) writeRoleErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, meta.ErrNotFound):
		writeErr(w, 404, "NOT_FOUND", "role not found", nil)
	case errors.Is(err, meta.ErrImmutable):
		writeErr(w, 403, "FORBIDDEN", "the builtin Admin role cannot be modified", nil)
	case errors.Is(err, meta.ErrInUse):
		writeErr(w, 400, "VALIDATION", "role is still assigned to users", nil)
	default:
		writeErr(w, 500, "INTERNAL", "server error", nil)
	}
}
