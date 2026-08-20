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

// RequirePlatform gates Platform Management (datasources, table definitions,
// audit). Admin passes implicitly (Admin role has platform_manage=1).
func (s *Server) RequirePlatform(next http.HandlerFunc) http.HandlerFunc {
	return s.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		if !userFrom(r).PlatformManage {
			writeErr(w, 403, "FORBIDDEN", "platform management requires a role with platform access", nil)
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
		"username": u.Username, "isAdmin": u.IsAdmin, "platformManage": u.PlatformManage,
	})
}
