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
		uid, ok, err := s.store.UserID(username)
		if err != nil || !ok {
			writeErr(w, 401, "AUTH", "unknown user", nil)
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserKey, CtxUser{ID: uid, Username: username})
		next(w, r.WithContext(ctx))
	}
}

func userFrom(r *http.Request) CtxUser {
	return r.Context().Value(ctxUserKey).(CtxUser)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct{ Username, Password string }
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	ok, err := s.store.VerifyUser(body.Username, body.Password)
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	if !ok {
		writeErr(w, 401, "AUTH", "invalid username or password", nil)
		return
	}
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
	writeJSON(w, 200, map[string]string{"username": u.Username})
}
