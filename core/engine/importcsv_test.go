package engine

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luthfi9251/kucrud-core/defs"
	"github.com/luthfi9251/kucrud-core/ds"
	"github.com/luthfi9251/kucrud-core/hooks"
)

// importDef is the import fixture: required name (editable), optional
// balance/status; the id key column is required but not editable (and key
// columns are exempt from import required-ness).
func importDef() *defs.Table {
	return &defs.Table{Name: "customers", Schema: "public", PhysTab: "customers",
		Keys: []string{"id"}, Columns: []defs.Column{
			{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true},
			{Name: "name", Label: "Name", FieldType: "text", Editable: true, Required: true},
			{Name: "balance", Label: "Balance", FieldType: "number", Editable: true},
			{Name: "status", Label: "Status", FieldType: "enum", EnumOptions: []string{"sunny", "rainy"}, Editable: true},
		}}
}

func importReq(csv string, fields map[string]string) *http.Request {
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
	r := httptest.NewRequest("POST", "/import", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return r
}

func doImport(svc *ImportService, td *defs.Table, csv string, fields map[string]string, apply bool) *httptest.ResponseRecorder {
	r := importReq(csv, fields)
	w := httptest.NewRecorder()
	if apply {
		svc.ApplyImport(w, r, td)
	} else {
		svc.PreviewImport(w, r, td)
	}
	return w
}

func TestImportPreview(t *testing.T) {
	var inserts [][]any
	res := &fakeResolver{tables: map[string]*defs.Table{"customers": importDef()}}
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		return &fakeAdapter{insert: func(sc, tb2 string, cols []string, vals []any) error {
			inserts = append(inserts, vals)
			return nil
		}}, nil
	}
	svc := &ImportService{R: res}

	csv := "name,balance,status\nnia,7.25,rainy\nbad,notanumber,sunny\nmissing,,snowy\n"
	w := doImport(svc, res.tables["customers"], csv, nil, false)
	if w.Code != 200 {
		t.Fatalf("preview = %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	for _, want := range []string{`"delimiter":","`, `"total":3`, `"valid":1`, `"invalid":2`,
		"not a number", "not in enum options", `"name":"nia"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("preview missing %q: %s", want, body)
		}
	}
	if len(inserts) != 0 {
		t.Fatalf("preview must not insert: %v", inserts)
	}

	// required column never mapped → every row invalid with that message
	w = doImport(svc, res.tables["customers"], "balance,status\n1,sunny\n",
		map[string]string{"mapping": `{"balance":"balance","status":"status"}`}, false)
	if !strings.Contains(w.Body.String(), "required column is not mapped") ||
		!strings.Contains(w.Body.String(), `"valid":0`) {
		t.Fatalf("unmapped required = %s", w.Body)
	}

	// semicolon delimiter sniffed and reported
	w = doImport(svc, res.tables["customers"], "name;status\nsemi;sunny\n", nil, false)
	if !strings.Contains(w.Body.String(), `"delimiter":";"`) || !strings.Contains(w.Body.String(), `"valid":1`) {
		t.Fatalf("semicolon = %s", w.Body)
	}

	// malformed multipart body → 400 IMPORT_BAD_CSV
	r := httptest.NewRequest("POST", "/import", strings.NewReader("junk"))
	r.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	svc.PreviewImport(w2, r, res.tables["customers"])
	if w2.Code != 400 || !strings.Contains(w2.Body.String(), "IMPORT_BAD_CSV") {
		t.Fatalf("bad body = %d %s", w2.Code, w2.Body)
	}
}

func TestImportApplyModes(t *testing.T) {
	var inserts [][]any
	res := &fakeResolver{tables: map[string]*defs.Table{"customers": importDef()}}
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		return &fakeAdapter{insert: func(sc, tb2 string, cols []string, vals []any) error {
			inserts = append(inserts, vals)
			return nil
		}}, nil
	}
	h := &fakeHooks{}
	svc := &ImportService{R: res, H: h}

	csv := "name,balance,status\nnia,7.25,rainy\nbad,notanumber,sunny\nmissing,,snowy\n"
	mapping := `{"name":"name","balance":"balance","status":"status"}`

	w := doImport(svc, res.tables["customers"], csv, map[string]string{"mapping": mapping, "mode": "valid"}, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"inserted":1`) ||
		!strings.Contains(w.Body.String(), `"failed":0`) || !strings.Contains(w.Body.String(), `"failures":[]`) {
		t.Fatalf("apply valid = %d %s", w.Code, w.Body)
	}
	if len(inserts) != 1 {
		t.Fatalf("valid mode inserts = %d", len(inserts))
	}
	// after-hook ran once for the inserted row, with the editable payload
	if len(h.afterSnap) != 1 || h.afterSnap[0].Values["name"] != "nia" ||
		h.afterSnap[0].Values["balance"] != 7.25 {
		t.Fatalf("after hooks = %+v", h.afterSnap)
	}

	// mode=all: invalid cells omitted, every row attempted → 3 inserts
	inserts = nil
	w = doImport(svc, res.tables["customers"], csv, map[string]string{"mapping": mapping, "mode": "all"}, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"inserted":3`) ||
		!strings.Contains(w.Body.String(), `"failed":0`) {
		t.Fatalf("apply all = %d %s", w.Code, w.Body)
	}
	if len(inserts) != 3 {
		t.Fatalf("all mode inserts = %d", len(inserts))
	}

	// insert failure lands in failures with the row index
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		return &fakeAdapter{insert: func(sc, tb2 string, cols []string, vals []any) error {
			return errFKViolation
		}, fkViolation: func(err error) bool { return err == errFKViolation }}, nil
	}
	w = doImport(svc, res.tables["customers"], "name\nx\n",
		map[string]string{"mapping": `{"name":"name"}`, "mode": "all"}, true)
	if !strings.Contains(w.Body.String(), `"inserted":0`) || !strings.Contains(w.Body.String(), `"failed":1`) ||
		!strings.Contains(w.Body.String(), "referenced row not found (database constraint)") {
		t.Fatalf("fk violation = %s", w.Body)
	}
}

var errFKViolation = &fakeErr{"fk violation"}

type fakeErr struct{ msg string }

func (e *fakeErr) Error() string { return e.msg }

func TestImportApplyValidation(t *testing.T) {
	res := &fakeResolver{tables: map[string]*defs.Table{"customers": importDef()}}
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) { return &fakeAdapter{}, nil }

	// mapping required on apply
	w := doImport(&ImportService{R: res}, res.tables["customers"], "name\nx\n", nil, true)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "mapping is required") {
		t.Fatalf("no mapping = %d %s", w.Code, w.Body)
	}
	// mode must be valid|all
	w = doImport(&ImportService{R: res}, res.tables["customers"], "name\nx\n",
		map[string]string{"mapping": `{"name":"name"}`, "mode": "nope"}, true)
	if w.Code != 400 || !strings.Contains(w.Body.String(), `mode must be \"valid\" or \"all\"`) {
		t.Fatalf("bad mode = %d %s", w.Code, w.Body)
	}
	// guard rejection before any parsing side effects
	guarded := &ImportService{R: res, H: &fakeHooks{guardErr: &hooks.MissingError{Name: "gone"}}}
	w = doImport(guarded, res.tables["customers"], "name\nx\n",
		map[string]string{"mapping": `{"name":"name"}`, "mode": "valid"}, true)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "HOOK_MISSING") {
		t.Fatalf("guard = %d %s", w.Code, w.Body)
	}
}

func TestImportFKCheck(t *testing.T) {
	tdef := importDef()
	tdef.Columns = append(tdef.Columns, defs.Column{Name: "region_id", Label: "Region",
		FieldType: "fk", BaseType: "number", Editable: true,
		FK: &defs.FK{Table: "regions", RefColumn: "id"}})
	res := &fakeResolver{tables: map[string]*defs.Table{"customers": tdef, "regions": regionsDef()}}
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		switch tb.Name {
		case "customers":
			return &fakeAdapter{insert: func(sc, tb2 string, cols []string, vals []any) error { return nil }}, nil
		case "regions":
			return &fakeAdapter{fetchByRefs: func(sc, tb, rc string, dc []string, vals []any) (map[string]map[string]any, error) {
				return map[string]map[string]any{"1": {"id": 1.0}}, nil
			}}, nil
		}
		return nil, &fakeErr{"unexpected table " + tb.Name}
	}
	svc := &ImportService{R: res}

	w := doImport(svc, tdef, "name,region_id\nok,1\ndangling,999\n", nil, false)
	if !strings.Contains(w.Body.String(), `"valid":1`) ||
		!strings.Contains(w.Body.String(), "referenced row not found") {
		t.Fatalf("fk check = %s", w.Body)
	}
}

func TestImportHooks(t *testing.T) {
	var inserts [][]any
	res := &fakeResolver{tables: map[string]*defs.Table{"customers": importDef()}}
	res.adapter = func(tb *defs.Table) (ds.Adapter, error) {
		return &fakeAdapter{insert: func(sc, tb2 string, cols []string, vals []any) error {
			inserts = append(inserts, vals)
			return nil
		}}, nil
	}
	h := &fakeHooks{before: func(ev hooks.Event, t *defs.Table, row hooks.RowPayload) (hooks.RowPayload, error) {
		if row.Values["name"] == "bob" {
			return row, errors.New("no bob allowed")
		}
		if row.Values["name"] == "mut" {
			row.Values["name"] = "mutated"
		}
		return row, nil
	}}
	svc := &ImportService{R: res, H: h}

	// preview runs before-hooks so rejections surface per row
	w := doImport(svc, res.tables["customers"], "name\nalice\nbob\n", nil, false)
	if !strings.Contains(w.Body.String(), `"valid":1`) ||
		!strings.Contains(w.Body.String(), "hook: no bob allowed") {
		t.Fatalf("preview hooks = %s", w.Body)
	}

	// apply: hook mutation lands in the insert; rejection → failure entry
	w = doImport(svc, res.tables["customers"], "name\nmut\nbob\n",
		map[string]string{"mapping": `{"name":"name"}`, "mode": "all"}, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"inserted":1`) ||
		!strings.Contains(w.Body.String(), `"failed":1`) ||
		!strings.Contains(w.Body.String(), "no bob allowed") {
		t.Fatalf("apply hooks = %d %s", w.Code, w.Body)
	}
	if len(inserts) != 1 || inserts[0][0] != "mutated" {
		t.Fatalf("mutations must land: %v", inserts)
	}
}
