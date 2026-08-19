package api

import (
	"errors"
	"net/http"
	"strings"

	"ku-crud/internal/meta"
)

type userDTO struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	RoleID   string `json:"roleId"`
	RoleName string `json:"roleName"`
	Disabled bool   `json:"disabled"`
	IsFirst  bool   `json:"isFirst"`
}

func validUsername(name string) bool {
	return name != "" && len(name) <= 64
}

func (s *Server) handleUserList(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListUsers()
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	out := make([]userDTO, len(list))
	for i, u := range list {
		out[i] = userDTO{s.ids.Encode("user", u.ID), u.Username,
			s.ids.Encode("role", u.RoleID), u.RoleName, u.Disabled, u.IsFirst}
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		RoleID   string `json:"roleId"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	if !validUsername(in.Username) || len(in.Password) < 4 {
		writeErr(w, 400, "VALIDATION", "username is required (max 64 chars) and password must be at least 4 characters", nil)
		return
	}
	roleID, err := s.ids.Decode("role", in.RoleID)
	if err != nil {
		writeErr(w, 400, "VALIDATION", "invalid roleId", nil)
		return
	}
	if _, _, err := s.store.GetRole(roleID); err != nil {
		writeErr(w, 400, "VALIDATION", "role not found", nil)
		return
	}
	if err := s.store.CreateUserWithRole(in.Username, in.Password, roleID); err != nil {
		writeErr(w, 400, "VALIDATION", "could not create user (duplicate username?)", err.Error())
		return
	}
	u, ok, _ := s.store.GetUserContext(in.Username)
	if !ok {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	writeJSON(w, 200, userDTO{s.ids.Encode("user", u.ID), u.Username,
		s.ids.Encode("role", u.RoleID), u.RoleName, false, u.IsFirst})
}

// handleUserUpdate changes password, role and disabled state. The first user
// is immutable; users cannot disable or delete themselves.
func (s *Server) handleUserUpdate(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r)
	id, err := s.ids.Decode("user", r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "NOT_FOUND", "user not found", nil)
		return
	}
	u, err := s.store.UserByID(id)
	if err != nil {
		s.writeUserErr(w, err)
		return
	}
	if u.IsFirst {
		writeErr(w, 403, "FORBIDDEN", "the first user cannot be modified", nil)
		return
	}
	if id == me.ID {
		writeErr(w, 400, "VALIDATION", "cannot modify your own account here", nil)
		return
	}
	var in struct {
		Password *string `json:"password"`
		RoleID   *string `json:"roleId"`
		Disabled *bool   `json:"disabled"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	var roleID *int64
	if in.RoleID != nil {
		v, err := s.ids.Decode("role", *in.RoleID)
		if err != nil {
			writeErr(w, 400, "VALIDATION", "invalid roleId", nil)
			return
		}
		if _, _, err := s.store.GetRole(v); err != nil {
			writeErr(w, 400, "VALIDATION", "role not found", nil)
			return
		}
		roleID = &v
	}
	var pw *string
	if in.Password != nil {
		if len(*in.Password) < 4 {
			writeErr(w, 400, "VALIDATION", "password must be at least 4 characters", nil)
			return
		}
		pw = in.Password
	}
	if err := s.store.UpdateUser(id, roleID, in.Disabled, pw); err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	s.writeUser(w, id)
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r)
	id, err := s.ids.Decode("user", r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "NOT_FOUND", "user not found", nil)
		return
	}
	u, err := s.store.UserByID(id)
	if err != nil {
		s.writeUserErr(w, err)
		return
	}
	if u.IsFirst {
		writeErr(w, 403, "FORBIDDEN", "the first user cannot be deleted", nil)
		return
	}
	if id == me.ID {
		writeErr(w, 400, "VALIDATION", "cannot delete your own account", nil)
		return
	}
	if err := s.store.DeleteUser(id); err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) writeUserErr(w http.ResponseWriter, err error) {
	if errors.Is(err, meta.ErrNotFound) {
		writeErr(w, 404, "NOT_FOUND", "user not found", nil)
		return
	}
	writeErr(w, 500, "INTERNAL", "server error", nil)
}

func (s *Server) writeUser(w http.ResponseWriter, id int64) {
	users, err := s.store.ListUsers()
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	for _, u := range users {
		if u.ID == id {
			writeJSON(w, 200, userDTO{s.ids.Encode("user", u.ID), u.Username,
				s.ids.Encode("role", u.RoleID), u.RoleName, u.Disabled, u.IsFirst})
			return
		}
	}
	writeErr(w, 404, "NOT_FOUND", "user not found", nil)
}
