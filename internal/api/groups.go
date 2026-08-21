package api

import (
	"errors"
	"net/http"

	"ku-crud/internal/meta"
)

type groupDTO struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

func (s *Server) groupNameMap() map[int64]string {
	m := map[int64]string{}
	gs, err := s.store.ListGroups()
	if err != nil {
		return m
	}
	for _, g := range gs {
		m[g.ID] = g.Name
	}
	return m
}

func (s *Server) handleGroupList(w http.ResponseWriter, r *http.Request) {
	gs, err := s.store.ListGroups()
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	out := []groupDTO{}
	for _, g := range gs {
		out = append(out, groupDTO{ID: s.ids.Encode("grp", g.ID), Name: g.Name, Position: g.Position})
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleGroupCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &in); err != nil || len(in.Name) < 1 || len(in.Name) > 60 {
		writeErr(w, 400, "VALIDATION", "group name must be 1..60 chars", nil)
		return
	}
	id, err := s.store.CreateGroup(in.Name)
	if errors.Is(err, meta.ErrGroupTaken) {
		writeErr(w, 409, "GROUP_NAME_TAKEN", "group name already exists", nil)
		return
	}
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	s.auditBestEffort(userFrom(r), 0, "GROUP_CREATE", in.Name, nil, nil)
	writeJSON(w, 200, groupDTO{ID: s.ids.Encode("grp", id), Name: in.Name})
}

func (s *Server) handleGroupUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := s.ids.Decode("grp", r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "NOT_FOUND", "group not found", nil)
		return
	}
	var in struct {
		Name *string `json:"name"`
		Move *string `json:"move"` // "up" | "down"
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	if in.Name != nil {
		if len(*in.Name) < 1 || len(*in.Name) > 60 {
			writeErr(w, 400, "VALIDATION", "group name must be 1..60 chars", nil)
			return
		}
		if err := s.store.RenameGroup(id, *in.Name); err != nil {
			if errors.Is(err, meta.ErrGroupTaken) {
				writeErr(w, 409, "GROUP_NAME_TAKEN", "group name already exists", nil)
				return
			}
			writeErr(w, 404, "NOT_FOUND", "group not found", nil)
			return
		}
	}
	if in.Move != nil {
		dir := 0
		switch *in.Move {
		case "up":
			dir = -1
		case "down":
			dir = 1
		}
		if dir == 0 {
			writeErr(w, 400, "VALIDATION", `move must be "up" or "down"`, nil)
			return
		}
		if err := s.store.MoveGroup(id, dir); err != nil {
			writeErr(w, 404, "NOT_FOUND", "group not found", nil)
			return
		}
	}
	s.auditBestEffort(userFrom(r), 0, "GROUP_UPDATE", "", nil, in)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleGroupDelete(w http.ResponseWriter, r *http.Request) {
	id, err := s.ids.Decode("grp", r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "NOT_FOUND", "group not found", nil)
		return
	}
	if err := s.store.DeleteGroup(id); errors.Is(err, meta.ErrNotFound) {
		writeErr(w, 404, "NOT_FOUND", "group not found", nil)
		return
	} else if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	s.auditBestEffort(userFrom(r), 0, "GROUP_DELETE", "", nil, nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// handleTableSetGroup moves a table between groups (groupId null = ungroup).
func (s *Server) handleTableSetGroup(w http.ResponseWriter, r *http.Request) {
	defID, err := s.ids.Decode("td", r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "NOT_FOUND", "table def not found", nil)
		return
	}
	var in struct {
		GroupID *string `json:"groupId"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	gid := int64(0)
	if in.GroupID != nil && *in.GroupID != "" {
		gid, err = s.ids.Decode("grp", *in.GroupID)
		if err != nil {
			writeErr(w, 400, "VALIDATION", "invalid groupId", nil)
			return
		}
	}
	if err := s.store.SetTableGroup(defID, gid); errors.Is(err, meta.ErrNotFound) {
		writeErr(w, 404, "NOT_FOUND", "table def not found", nil)
		return
	} else if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
