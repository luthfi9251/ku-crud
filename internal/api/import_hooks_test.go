package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/luthfi9251/ku-crud/core/engine"
	"github.com/luthfi9251/ku-crud/core/hooks"
)

// noBobReg registers NoBob (rejects name=="bob" rows, tag 3 links being
// added, tag 2 links being removed) and NoteAfter (counting after-hook).
func noBobReg(runs *int) *hooks.Registry {
	tagID := func(m map[string]any) string {
		if m == nil {
			return ""
		}
		return fmt.Sprint(m["tag_id"])
	}
	reg := hooks.NewRegistry()
	reg.Register("NoBob", func(ctx context.Context, hc *hooks.HookContext, ev hooks.Event, row hooks.RowPayload, cfg json.RawMessage) (hooks.RowPayload, error) {
		if row.Values["name"] == "bob" || row.Old["name"] == "bob" {
			return row, errors.New("no bob allowed")
		}
		if tagID(row.Values) == "3" || tagID(row.Old) == "2" {
			return row, errors.New("tag link not allowed")
		}
		return row, nil
	})
	reg.Register("NoteAfter", func(ctx context.Context, hc *hooks.HookContext, ev hooks.Event, row hooks.RowPayload, cfg json.RawMessage) (hooks.RowPayload, error) {
		*runs++
		return row, nil
	})
	return reg
}

func doImport(t *testing.T, s *Server, c *string, path, csv string, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := importRequest(t, path, csv, fields)
	req.Header.Set("Cookie", *c)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	return w
}

func TestImportPreviewRunsBeforeHooks(t *testing.T) {
	if os.Getenv("KUCRUD_TEST_PG") == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	runs := 0
	s := newTestServerHooks(t, noBobReg(&runs))
	c := login(s)
	seedLive(t, s)
	setHooks(t, s, 1, `{"beforeCreate":[{"hook":"NoBob","order":1}]}`)

	w := doImport(t, s, c, "/api/data/"+defName(s, 1)+"/import/preview", "name,status\nalice,sunny\nbob,sunny\n", nil)
	if w.Code != 200 {
		t.Fatalf("preview = %d %s", w.Code, w.Body)
	}
	var res struct {
		Counts map[string]int `json:"counts"`
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Counts["valid"] != 1 || res.Counts["invalid"] != 1 {
		t.Fatalf("counts = %v (bob must be invalid)", res.Counts)
	}
	if !strings.Contains(w.Body.String(), "no bob allowed") {
		t.Fatalf("hook message missing: %s", w.Body)
	}
}

func TestImportApplyHookRejectAndEnqueue(t *testing.T) {
	if os.Getenv("KUCRUD_TEST_PG") == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	runs := 0
	s := newTestServerHooks(t, noBobReg(&runs))
	c := login(s)
	seedLive(t, s)
	setHooks(t, s, 1, `{"beforeCreate":[{"hook":"NoBob","order":1}],"afterCreate":[{"hook":"NoteAfter","order":1}]}`)

	w := doImport(t, s, c, "/api/data/"+defName(s, 1)+"/import/apply",
		"name,status\nalice,sunny\nbob,sunny\n", map[string]string{
			"mapping": `{"name":"name","status":"status"}`,
			"mode":    "all",
		})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"inserted":1`) || !strings.Contains(w.Body.String(), `"failed":1`) {
		t.Fatalf("apply = %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "no bob allowed") {
		t.Fatalf("hook failure detail missing: %s", w.Body)
	}
	if w := do(s, "GET", "/api/data/"+defName(s, 1)+"/rows?search=bob", "", c); strings.Contains(w.Body.String(), "bob") {
		t.Fatal("rejected row must not be inserted")
	}
	// afterCreate enqueued only for the inserted row
	entries, total, _ := s.store.ListOutbox("", 0, 10, 0)
	if total != 1 || entries[0].Event != "afterCreate" || entries[0].HookName != "NoteAfter" {
		t.Fatalf("outbox = %d %+v", total, entries)
	}
	if !strings.Contains(entries[0].NewValues, "alice") {
		t.Fatalf("outbox snapshot = %s", entries[0].NewValues)
	}
}

func TestBulkDeleteHookReject(t *testing.T) {
	if os.Getenv("KUCRUD_TEST_PG") == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	runs := 0
	s := newTestServerHooks(t, noBobReg(&runs))
	c := login(s)
	seedLive(t, s) // rows: 1=jo, 2=joe, 3=ana
	do(s, "PUT", "/api/data/"+defName(s, 1)+"/rows/"+engine.EncodeRowKey([]string{"2"}), `{"name":"bob"}`, c)
	setHooks(t, s, 1, `{"beforeDelete":[{"hook":"NoBob","order":1}],"afterDelete":[{"hook":"NoteAfter","order":1}]}`)

	keys := engine.EncodeRowKey([]string{"2"}) + `","` + engine.EncodeRowKey([]string{"3"})
	w := do(s, "POST", "/api/data/"+defName(s, 1)+"/rows/bulk-delete", `{"keys":["`+keys+`"]}`, c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"deleted":1`) || !strings.Contains(w.Body.String(), `"failed":1`) {
		t.Fatalf("bulk = %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "HOOK_REJECTED") || !strings.Contains(w.Body.String(), "no bob allowed") {
		t.Fatalf("hook failure = %s", w.Body)
	}
	if w := do(s, "GET", "/api/data/"+defName(s, 1)+"/rows?search=bob", "", c); !strings.Contains(w.Body.String(), "bob") {
		t.Fatal("rejected row must survive")
	}
	// afterDelete enqueued only for the deleted row (ana)
	entries, total, _ := s.store.ListOutbox("", 0, 10, 0)
	if total != 1 || entries[0].Event != "afterDelete" {
		t.Fatalf("outbox = %d %+v", total, entries)
	}
	if !strings.Contains(entries[0].OldValues, "ana") {
		t.Fatalf("old snapshot = %s", entries[0].OldValues)
	}
}

func TestM2MJunctionHookReject(t *testing.T) {
	if os.Getenv("KUCRUD_TEST_PG") == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	runs := 0
	s := newTestServerHooks(t, noBobReg(&runs))
	c := login(s)
	custTok, _ := seedM2M(t, s) // junction def 3; jo(1) has tags 1,2
	addM2MColumn(t, s, c, custTok, tdTok(s, 2))
	m2mURL := "/api/data/customers/rows/" + engine.EncodeRowKey([]string{"1"}) + "/m2m/m2m_tags"

	// add tag 3 → junction beforeCreate rejects the link insert
	setHooks(t, s, 3, `{"beforeCreate":[{"hook":"NoBob","order":1}]}`)
	w := do(s, "PUT", "/api/data/customers/rows/"+engine.EncodeRowKey([]string{"1"}),
		`{"name":"jo","m2m_tags":[1,2,3]}`, c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "HOOK_REJECTED") {
		t.Fatalf("m2m add reject = %d %s", w.Code, w.Body)
	}
	if w = do(s, "GET", m2mURL, "", c); !strings.Contains(w.Body.String(), `"values":[1,2]`) {
		t.Fatalf("tag 3 must not be linked: %s", w.Body)
	}

	// remove tag 2 → junction beforeDelete rejects the link delete
	setHooks(t, s, 3, `{"beforeDelete":[{"hook":"NoBob","order":1}]}`)
	w = do(s, "PUT", "/api/data/customers/rows/"+engine.EncodeRowKey([]string{"1"}),
		`{"name":"jo","m2m_tags":[1]}`, c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "HOOK_REJECTED") {
		t.Fatalf("m2m remove reject = %d %s", w.Code, w.Body)
	}
	if w = do(s, "GET", m2mURL, "", c); !strings.Contains(w.Body.String(), `"values":[1,2]`) {
		t.Fatalf("tag 2 must survive: %s", w.Body)
	}
}
