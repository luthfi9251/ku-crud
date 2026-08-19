package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"ku-crud/internal/meta"
)

type Server struct {
	store *meta.Store
}

func New(store *meta.Store) *Server { return &Server{store: store} }

type CtxUser struct {
	ID       int64
	Username string
}

type ctxKey int

const ctxUserKey ctxKey = 0

func writeErr(w http.ResponseWriter, status int, code, msg string, detail any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"code": code, "message": msg, "detail": detail})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return errors.New("invalid JSON body")
	}
	return nil
}

func notImplemented(w http.ResponseWriter, r *http.Request) {
	writeErr(w, http.StatusNotImplemented, "INTERNAL", "not implemented", nil)
}

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/auth/me", s.RequireAuth(s.handleMe))

	mux.HandleFunc("GET /api/datasources", s.RequireAuth(s.handleDSList))
	mux.HandleFunc("POST /api/datasources", s.RequireAuth(s.handleDSCreate))
	mux.HandleFunc("PUT /api/datasources/{id}", s.RequireAuth(s.handleDSUpdate))
	mux.HandleFunc("DELETE /api/datasources/{id}", s.RequireAuth(s.handleDSDelete))
	mux.HandleFunc("POST /api/datasources/{id}/test", s.RequireAuth(s.handleDSTest))
	mux.HandleFunc("GET /api/datasources/{id}/tables", s.RequireAuth(s.handleDSTables))
	mux.HandleFunc("GET /api/datasources/{id}/tables/{schema}/{table}/columns", s.RequireAuth(s.handleDSColumns))

	mux.HandleFunc("GET /api/tables", s.RequireAuth(s.handleTableList))
	mux.HandleFunc("POST /api/tables", s.RequireAuth(s.handleTableCreate))
	mux.HandleFunc("GET /api/tables/{id}", s.RequireAuth(s.handleTableGet))
	mux.HandleFunc("PUT /api/tables/{id}", s.RequireAuth(s.handleTableUpdate))
	mux.HandleFunc("DELETE /api/tables/{id}", s.RequireAuth(s.handleTableDelete))
	mux.HandleFunc("GET /api/tables/{id}/verify", s.RequireAuth(notImplemented))
	mux.HandleFunc("POST /api/tables/{id}/resync", s.RequireAuth(notImplemented))
	mux.HandleFunc("GET /api/tables/{id}/rows", s.RequireAuth(s.handleRowList))
	mux.HandleFunc("POST /api/tables/{id}/rows", s.RequireAuth(notImplemented))
	mux.HandleFunc("GET /api/tables/{id}/rows/{pk}", s.RequireAuth(s.handleRowGet))
	mux.HandleFunc("PUT /api/tables/{id}/rows/{pk}", s.RequireAuth(notImplemented))
	mux.HandleFunc("DELETE /api/tables/{id}/rows/{pk}", s.RequireAuth(notImplemented))
	mux.HandleFunc("GET /api/audit", s.RequireAuth(notImplemented))
	return mux
}
