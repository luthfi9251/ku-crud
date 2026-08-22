package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const cookieName = "ku_session"
const sessionTTL = 24 * time.Hour

func signSession(secret []byte, username string, exp int64) string {
	msg := fmt.Sprintf("%s|%d", username, exp)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(msg))
	return msg + "|" + hex.EncodeToString(mac.Sum(nil))
}

func parseSession(secret []byte, v string) (string, bool) {
	parts := strings.Split(v, "|")
	if len(parts) != 3 {
		return "", false
	}
	username, expS, sig := parts[0], parts[1], parts[2]
	exp, err := strconv.ParseInt(expS, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return "", false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(username + "|" + expS))
	want := hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(want), []byte(sig)) != 1 {
		return "", false
	}
	return username, true
}

func (s *Server) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil {
			writeErr(w, 401, "AUTH", "login required", nil)
			return
		}
		secret, err := s.store.SessionSecret()
		if err != nil {
			writeErr(w, 500, "INTERNAL", "server error", nil)
			return
		}
		username, ok := parseSession(secret, c.Value)
		if !ok {
			writeErr(w, 401, "AUTH", "invalid or expired session", nil)
			return
		}
		u, ok, err := s.store.GetUserContext(username)
		if err != nil {
			writeErr(w, 500, "INTERNAL", "server error", nil)
			return
		}
		if !ok {
			writeErr(w, 401, "AUTH", "unknown or disabled user", nil)
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserKey, u)
		next(w, r.WithContext(ctx))
	}
}

// RequireDSManage gates datasource management. Admin passes implicitly.
func (s *Server) RequireDSManage(next http.HandlerFunc) http.HandlerFunc {
	return s.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		u := userFrom(r)
		if !u.IsAdmin && !u.ManageDatasources {
			writeErr(w, 403, "FORBIDDEN", "datasource management requires a role with datasource access", nil)
			return
		}
		next(w, r)
	})
}

// RequireTablesManage gates table-definition management (including the
// hooks dropdown that feeds the definition editor). Admin passes implicitly.
func (s *Server) RequireTablesManage(next http.HandlerFunc) http.HandlerFunc {
	return s.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		u := userFrom(r)
		if !u.IsAdmin && !u.ManageTables {
			writeErr(w, 403, "FORBIDDEN", "table definition management requires a role with definition access", nil)
			return
		}
		next(w, r)
	})
}

// RequireAuditView gates the audit trail. Admin passes implicitly.
func (s *Server) RequireAuditView(next http.HandlerFunc) http.HandlerFunc {
	return s.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		u := userFrom(r)
		if !u.IsAdmin && !u.ViewAudit {
			writeErr(w, 403, "FORBIDDEN", "audit trail requires a role with audit access", nil)
			return
		}
		next(w, r)
	})
}

// RequireOutboxView gates the hook outbox monitor. Admin passes implicitly.
func (s *Server) RequireOutboxView(next http.HandlerFunc) http.HandlerFunc {
	return s.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		u := userFrom(r)
		if !u.IsAdmin && !u.ViewOutbox {
			writeErr(w, 403, "FORBIDDEN", "hook outbox requires a role with outbox access", nil)
			return
		}
		next(w, r)
	})
}

// RequirePlatformAll gates endpoints that touch datasources AND definitions
// (meta transfer). Admin passes implicitly.
func (s *Server) RequirePlatformAll(next http.HandlerFunc) http.HandlerFunc {
	return s.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		u := userFrom(r)
		if !u.IsAdmin && !(u.ManageDatasources && u.ManageTables) {
			writeErr(w, 403, "FORBIDDEN", "definition transfer requires both datasource and definition access", nil)
			return
		}
		next(w, r)
	})
}

// RequireAdmin gates user & role management (builtin Admin only).
func (s *Server) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		if !userFrom(r).IsAdmin {
			writeErr(w, 403, "FORBIDDEN", "admin role required", nil)
			return
		}
		next(w, r)
	})
}

func userFrom(r *http.Request) CtxUser {
	return r.Context().Value(ctxUserKey).(CtxUser)
}

// limitKey scopes a credential attempt to username+client IP so one
// account (or one host) cannot be hammered from everywhere at once.
func limitKey(username string, r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return strings.ToLower(username) + "|" + host
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct{ Username, Password string }
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	key := limitKey(body.Username, r)
	if !s.loginLimit.allow(key) {
		writeErr(w, 429, "RATE_LIMITED", "too many failed attempts; try again later", nil)
		return
	}
	ok, err := s.store.VerifyUser(body.Username, body.Password)
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	if !ok {
		s.loginLimit.fail(key)
		writeErr(w, 401, "AUTH", "invalid username or password", nil)
		return
	}
	s.loginLimit.reset(key)
	secret, err := s.store.SessionSecret()
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	v := signSession(secret, body.Username, time.Now().Add(sessionTTL).Unix())
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: v, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
		MaxAge: int(sessionTTL.Seconds()),
	})
	w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
	w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	writeJSON(w, 200, map[string]any{
		"username": u.Username, "isAdmin": u.IsAdmin,
		"manageDatasources": u.ManageDatasources, "manageTables": u.ManageTables,
		"viewAudit": u.ViewAudit, "viewOutbox": u.ViewOutbox,
		"language": u.Language,
	})
}

func (s *Server) handleMeUpdate(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var in struct {
		Language string `json:"language"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	if in.Language != "en" && in.Language != "id" {
		writeErr(w, 400, "VALIDATION", "language must be en or id", nil)
		return
	}
	if err := s.store.UpdateUserLanguage(u.ID, in.Language); err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	writeJSON(w, 200, map[string]string{"language": in.Language})
}
