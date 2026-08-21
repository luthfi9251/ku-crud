package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"ku-crud/internal/meta"
	"ku-crud/internal/tokenid"
)

type Server struct {
	store *meta.Store
	ids   *tokenid.Codec
	// loginLimit throttles credential endpoints (brute-force protection);
	// in-memory, per instance.
	loginLimit *loginLimiter
}

func New(store *meta.Store) (*Server, error) {
	secret, err := store.IDSecret()
	if err != nil {
		return nil, err
	}
	return &Server{store: store, ids: tokenid.New(secret),
		loginLimit: newLoginLimiter(5, 15*time.Minute)}, nil
}

// CtxUser is the per-request auth context (role included).
type CtxUser = meta.UserCtx

type ctxKey int

const ctxUserKey ctxKey = 0

// respInfo lets writeErr stash the app error on the logging middleware's
// response writer so the access log can include code/message for non-2xx.
type respInfo interface{ setError(code, msg string) }

func writeErr(w http.ResponseWriter, status int, code, msg string, detail any) {
	if ri, ok := w.(respInfo); ok {
		ri.setError(code, msg)
	}
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

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/auth/me", s.RequireAuth(s.handleMe))

	mux.HandleFunc("GET /api/setup/status", s.handleSetupStatus)
	mux.HandleFunc("POST /api/setup", s.handleSetup)

	mux.HandleFunc("GET /api/datasources", s.RequirePlatform(s.handleDSList))
	mux.HandleFunc("POST /api/datasources", s.RequirePlatform(s.handleDSCreate))
	mux.HandleFunc("PUT /api/datasources/{id}", s.RequirePlatform(s.handleDSUpdate))
	mux.HandleFunc("DELETE /api/datasources/{id}", s.RequirePlatform(s.handleDSDelete))
	mux.HandleFunc("POST /api/datasources/{id}/test", s.RequirePlatform(s.handleDSTest))
	mux.HandleFunc("GET /api/datasources/{id}/tables", s.RequirePlatform(s.handleDSTables))
	mux.HandleFunc("GET /api/datasources/{id}/tables/{schema}/{table}/columns", s.RequirePlatform(s.handleDSColumns))

	mux.HandleFunc("GET /api/tables", s.RequireAuth(s.handleTableList))
	mux.HandleFunc("POST /api/tables", s.RequirePlatform(s.handleTableCreate))
	mux.HandleFunc("GET /api/tables/{id}", s.RequireAuth(s.handleTableGet))
	mux.HandleFunc("PUT /api/tables/{id}", s.RequirePlatform(s.handleTableUpdate))
	mux.HandleFunc("DELETE /api/tables/{id}", s.RequirePlatform(s.handleTableDelete))
	mux.HandleFunc("GET /api/tables/{id}/verify", s.RequirePlatform(s.handleVerify))
	mux.HandleFunc("POST /api/tables/{id}/resync", s.RequirePlatform(s.handleResync))

	mux.HandleFunc("GET /api/meta/export", s.RequirePlatform(s.handleMetaExport))
	mux.HandleFunc("POST /api/meta/import/preview", s.RequirePlatform(s.handleMetaImportPreview))
	mux.HandleFunc("POST /api/meta/import/apply", s.RequirePlatform(s.handleMetaImportApply))

	mux.HandleFunc("GET /api/groups", s.RequireAuth(s.handleGroupList))
	mux.HandleFunc("POST /api/groups", s.RequirePlatform(s.handleGroupCreate))
	mux.HandleFunc("PATCH /api/groups/{id}", s.RequirePlatform(s.handleGroupUpdate))
	mux.HandleFunc("DELETE /api/groups/{id}", s.RequirePlatform(s.handleGroupDelete))
	mux.HandleFunc("PATCH /api/tables/{id}", s.RequirePlatform(s.handleTableSetGroup))

	mux.HandleFunc("GET /api/tables/{id}/rows", s.RequireAuth(s.handleRowList))
	mux.HandleFunc("POST /api/tables/{id}/rows", s.RequireAuth(s.handleRowCreate))
	mux.HandleFunc("GET /api/tables/{id}/rows/{pk}", s.RequireAuth(s.handleRowGet))
	mux.HandleFunc("PUT /api/tables/{id}/rows/{pk}", s.RequireAuth(s.handleRowUpdate))
	mux.HandleFunc("DELETE /api/tables/{id}/rows/{pk}", s.RequireAuth(s.handleRowDelete))
	mux.HandleFunc("POST /api/tables/{id}/rows/bulk-delete", s.RequireAuth(s.handleRowBulkDelete))
	mux.HandleFunc("GET /api/tables/{id}/fkoptions/{column}", s.RequireAuth(s.handleFKOptions))
	mux.HandleFunc("GET /api/tables/{id}/m2moptions/{column}", s.RequireAuth(s.handleM2MOptions))
	mux.HandleFunc("GET /api/tables/{id}/rows/{pk}/m2m/{column}", s.RequireAuth(s.handleM2MLinks))
	mux.HandleFunc("GET /api/tables/{id}/rows/export", s.RequireAuth(s.handleRowExport))
	mux.HandleFunc("POST /api/tables/{id}/import/preview", s.RequireAuth(s.handleImportPreview))
	mux.HandleFunc("POST /api/tables/{id}/import/apply", s.RequireAuth(s.handleImportApply))
	mux.HandleFunc("GET /api/tables/{id}/saved-filters", s.RequireAuth(s.handleSavedFilterList))
	mux.HandleFunc("POST /api/tables/{id}/saved-filters", s.RequireAuth(s.handleSavedFilterCreate))
	mux.HandleFunc("PUT /api/tables/{id}/saved-filters/{fid}", s.RequireAuth(s.handleSavedFilterUpdate))
	mux.HandleFunc("DELETE /api/tables/{id}/saved-filters/{fid}", s.RequireAuth(s.handleSavedFilterDelete))
	mux.HandleFunc("GET /api/audit", s.RequirePlatform(s.handleAuditList))

	mux.HandleFunc("GET /api/users", s.RequireAdmin(s.handleUserList))
	mux.HandleFunc("POST /api/users", s.RequireAdmin(s.handleUserCreate))
	mux.HandleFunc("PUT /api/users/{id}", s.RequireAdmin(s.handleUserUpdate))
	mux.HandleFunc("DELETE /api/users/{id}", s.RequireAdmin(s.handleUserDelete))

	mux.HandleFunc("GET /api/roles", s.RequireAdmin(s.handleRoleList))
	mux.HandleFunc("POST /api/roles", s.RequireAdmin(s.handleRoleCreate))
	mux.HandleFunc("PUT /api/roles/{id}", s.RequireAdmin(s.handleRoleUpdate))
	mux.HandleFunc("DELETE /api/roles/{id}", s.RequireAdmin(s.handleRoleDelete))
	return mux
}
