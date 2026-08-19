package api

import (
	"net/http"
)

// First-run setup endpoints. Deliberately unauthenticated: they exist only
// before the first user does. POST is atomic-locked via CreateFirstUser —
// once any user exists it permanently returns 403.
func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.CountUsers()
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	writeJSON(w, 200, map[string]bool{"needed": n == 0})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var body struct{ Username, Password string }
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	if body.Username == "" || len(body.Password) < 4 {
		writeErr(w, 400, "VALIDATION", "username is required and password must be at least 4 characters", nil)
		return
	}
	created, err := s.store.CreateFirstUser(body.Username, body.Password)
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	if !created {
		writeErr(w, 403, "AUTH", "setup already completed", nil)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
