package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

// importRequest builds a multipart request for the import endpoints.
func importRequest(t *testing.T, path, csv string, fields map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if csv != "" {
		fw, _ := mw.CreateFormFile("file", "import.csv")
		fw.Write([]byte(csv))
	}
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	mw.Close()
	req := httptest.NewRequest("POST", path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestImportPreviewAndApply(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedLive(t, s) // customers: name required, status enum, balance numeric
	tok := tdTok(s, 1)

	csv := "name,balance,born,status\nnia,7.25,1990-01-02,rainy\nbad,notanumber,1990-01-02,sunny\nmissing,,1990-01-02,snowy\n"
	req := importRequest(t, "/api/tables/"+tok+"/import/preview", csv, nil)
	req.Header.Set("Cookie", *c)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("preview = %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"total":3`) || !strings.Contains(body, `"valid":1`) || !strings.Contains(body, `"invalid":2`) {
		t.Fatalf("counts = %s", body)
	}
	if !strings.Contains(body, "not a number") || !strings.Contains(body, "not in enum options") {
		t.Fatalf("per-row errors = %s", body)
	}

	// apply valid-only: one row inserted, others skipped
	req = importRequest(t, "/api/tables/"+tok+"/import/apply", csv, map[string]string{
		"mapping": `{"name":"name","balance":"balance","born":"born","status":"status"}`,
		"mode":    "valid",
	})
	req.Header.Set("Cookie", *c)
	w = httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"inserted":1`) || !strings.Contains(w.Body.String(), `"failed":0`) {
		t.Fatalf("apply valid = %d %s", w.Code, w.Body)
	}

	// row actually landed
	if w := do(s, "GET", "/api/tables/"+tok+"/rows?search=nia", "", c); !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatalf("inserted row missing: %s", w.Body)
	}

	// audit returns in Task 11 (platformhooks): the import INSERT audit
	// assertion was removed with the write path's audit decoupling.

	// apply mode=all: invalid cells are omitted (best-effort NULL) and the
	// rows are still attempted — all three rows insert (nia again), none fail
	req = importRequest(t, "/api/tables/"+tok+"/import/apply", csv, map[string]string{
		"mapping": `{"name":"name","balance":"balance","born":"born","status":"status"}`,
		"mode":    "all",
	})
	req.Header.Set("Cookie", *c)
	w = httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("apply all = %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"inserted":3`) || !strings.Contains(w.Body.String(), `"failed":0`) {
		t.Fatalf("apply all result = %s", w.Body)
	}
}

func TestImportSemicolonFile(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedLive(t, s)
	tok := tdTok(s, 1)

	req := importRequest(t, "/api/tables/"+tok+"/import/preview", "name;status\nsemi;sunny\n", nil)
	req.Header.Set("Cookie", *c)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"valid":1`) || !strings.Contains(w.Body.String(), `";"`) {
		t.Fatalf("semicolon preview = %d %s", w.Code, w.Body)
	}
}

func TestImportRequiresCreateGrant(t *testing.T) {
	s := newTestServer(t)
	seedLive(t, s)
	tok := tdTok(s, 1)

	reader := loginAs(t, s, "reader2", &meta.Role{Name: "Reader2"},
		[]meta.TableGrant{{TableDefID: 1, CanRead: true, CanCreate: false}})
	req := importRequest(t, "/api/tables/"+tok+"/import/preview", "name\nx\n", nil)
	req.Header.Set("Cookie", *reader)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("no-create preview = %d %s", w.Code, w.Body)
	}
}

func TestImportFKValidation(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedFKLive(t, s) // orders def 2: note text, customer_id fk → customers.id

	csv := "note,customer_id\no9,1\no10,999\n"
	req := importRequest(t, "/api/tables/"+tdTok(s, 2)+"/import/preview", csv, nil)
	req.Header.Set("Cookie", *c)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("preview = %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"valid":1`) || !strings.Contains(w.Body.String(), "referenced row not found") {
		t.Fatalf("fk validation = %s", w.Body)
	}
}
