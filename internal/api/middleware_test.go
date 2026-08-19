package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func captureLogs(t *testing.T, s *Server, method, path string) (*httptest.ResponseRecorder, []map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(old)
	h := s.withLogging(s.Routes())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	out := []map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		json.Unmarshal([]byte(line), &m)
		out = append(out, m)
	}
	return w, out
}

func TestLoggingMiddlewareFields(t *testing.T) {
	s := newTestServer(t)

	// 200: info line with method/path/status/dur_ms, no error code
	_, logs := captureLogs(t, s, "GET", "/api/health")
	if len(logs) != 1 {
		t.Fatalf("want 1 log line, got %d", len(logs))
	}
	l := logs[0]
	if l["method"] != "GET" || l["path"] != "/api/health" || l["status"] != float64(200) {
		t.Fatalf("log=%v", l)
	}
	if _, ok := l["dur_ms"]; !ok {
		t.Fatal("missing dur_ms")
	}
	if _, ok := l["code"]; ok {
		t.Fatalf("unexpected code on 200: %v", l)
	}

	// 401 (writeErr path): warn level, code+message attached
	_, logs = captureLogs(t, s, "GET", "/api/tables")
	l = logs[0]
	if l["level"] != "WARN" || l["status"] != float64(401) {
		t.Fatalf("401 log=%v", l)
	}
	if l["code"] != "AUTH" || l["message"] == "" {
		t.Fatalf("401 log missing error detail: %v", l)
	}

	// 404 without writeErr (unknown route): still logged
	w, logs := captureLogs(t, s, "GET", "/api/nope")
	if w.Code != 404 || logs[0]["status"] != float64(404) {
		t.Fatalf("404 log=%v", logs[0])
	}
}
