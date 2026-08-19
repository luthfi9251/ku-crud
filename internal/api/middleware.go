package api

import (
	"log/slog"
	"net/http"
	"time"
)

// statusWriter captures the response status and the app error (stashed by
// writeErr) so the access log can attribute non-2xx responses.
type statusWriter struct {
	http.ResponseWriter
	status int
	code   string
	msg    string
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) setError(code, msg string) {
	w.code, w.msg = code, msg
}

// withLogging emits one slog line per request: method, path, status and
// duration; non-2xx responses also carry the app error code/message
// (4xx at WARN, 5xx at ERROR — brief asks for errors on non-200, warn keeps
// unauthenticated probes from flooding the error stream).
func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = 200
		}
		lvl := slog.LevelInfo
		if sw.status >= 500 {
			lvl = slog.LevelError
		} else if sw.status >= 400 {
			lvl = slog.LevelWarn
		}
		args := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"dur_ms", float64(time.Since(start).Microseconds()) / 1000.0,
		}
		if sw.status >= 400 && sw.code != "" {
			args = append(args, "code", sw.code, "message", sw.msg)
		}
		slog.Log(r.Context(), lvl, "request", args...)
	})
}

// WithLogging is the exported wrapper used by cmd/main.go.
func (s *Server) WithLogging(next http.Handler) http.Handler {
	return s.withLogging(next)
}
