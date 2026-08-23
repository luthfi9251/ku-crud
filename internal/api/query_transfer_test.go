package api

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

func TestMetaTransferQueryDef(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	seedQueryDef(t, s, []string{"n"}) // ds "dead" id 1 + query def id 1

	w := do(s, "GET", "/api/meta/export", "", c)
	if w.Code != 200 {
		t.Fatalf("export = %d %s", w.Code, w.Body)
	}
	var file struct {
		Tables []struct {
			SourceType string `json:"sourceType"`
			QuerySQL   string `json:"querySql"`
		} `json:"tables"`
	}
	json.Unmarshal(w.Body.Bytes(), &file)
	if len(file.Tables) != 1 || file.Tables[0].SourceType != "query" ||
		file.Tables[0].QuerySQL != "SELECT name AS n FROM customers" {
		t.Fatalf("export tables = %s", w.Body)
	}

	// import the same bundle into a second instance with the same local ds name
	s2 := newTestServer(t)
	c2 := login(s2)
	if err := s2.store.CreateDatasource(&meta.Datasource{Name: "dead", Host: "x", Port: 1,
		DBName: "x", Username: "x", Password: "x", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	bb, _ := json.Marshal(map[string]any{
		"format": "ku-crud-meta", "version": 1,
		"groups": []string{}, "datasources": []map[string]any{},
		"tables": []map[string]any{{
			"datasourceRef": "dead", "schema": "", "table": "", "label": "Q",
			"keyColumns": []string{"n"}, "pageSize": 20, "sourceType": "query",
			"querySql": "SELECT name AS n FROM customers", "defaultSortDir": "ASC",
			"columns": []map[string]any{{"name": "n", "label": "N", "fieldType": "text",
				"editable": false, "required": false, "visible": true, "searchable": true,
				"sortable": true, "position": 0}},
		}}})

	// preview: a query def is keyed "ds/query/<label>" (empty schema/table
	// must not collapse two query defs into one ref) and is new here
	req := multipartBody(t, "/api/meta/import/preview", "file", "x.json", bb)
	req.Header.Set("Cookie", *c2)
	resp := httptest.NewRecorder()
	s2.Routes().ServeHTTP(resp, req)
	if resp.Code != 200 {
		t.Fatalf("preview = %d %s", resp.Code, resp.Body)
	}
	var pr importPreviewRes
	json.Unmarshal(resp.Body.Bytes(), &pr)
	if len(pr.Tables) != 1 || pr.Tables[0].Ref != "dead/query/Q" || pr.Tables[0].Status != "new" {
		t.Fatalf("preview tables = %+v", pr.Tables)
	}

	// apply: spec §9 — EXPLAIN validation runs against the selected local
	// datasource; "dead" is unreachable, so the apply is rejected and the
	// store stays untouched (all-or-nothing)
	resp = applyMeta(t, s2, c2, string(bb),
		`{"datasources":[],"tables":[{"ref":"dead/query/Q","mode":"overwrite"}],"groups":false}`)
	if resp.Code != 400 || !strings.Contains(resp.Body.String(), "META_IMPORT_INVALID") {
		t.Fatalf("apply against dead ds = %d %s", resp.Code, resp.Body)
	}
	if defs, _ := s2.store.ListTableDefs(); len(defs) != 0 {
		t.Fatalf("rejected apply must not create defs: %+v", defs)
	}

	// seed the def at store level (SaveTableDef defaults defaultSortDir to
	// ASC, matching the bundle) so the label-ref diff logic stays covered
	def := &meta.TableDef{DatasourceID: 1, SourceType: "query",
		QuerySQL: "SELECT name AS n FROM customers", Label: "Q",
		KeyColumns: []string{"n"}, PageSize: 20, DefaultSortDir: "ASC"}
	cols := []meta.ColumnDef{{Name: "n", Label: "N", FieldType: "text",
		Visible: true, Searchable: true, Sortable: true, Position: 0}}
	if err := s2.store.SaveTableDef(def, cols); err != nil {
		t.Fatal(err)
	}

	// re-preview: identical roundtrip is recognized under the same ref
	req = multipartBody(t, "/api/meta/import/preview", "file", "x.json", bb)
	req.Header.Set("Cookie", *c2)
	resp = httptest.NewRecorder()
	s2.Routes().ServeHTTP(resp, req)
	pr = importPreviewRes{}
	json.Unmarshal(resp.Body.Bytes(), &pr)
	if len(pr.Tables) != 1 || pr.Tables[0].Ref != "dead/query/Q" || pr.Tables[0].Status != "duplicate-identical" {
		t.Fatalf("re-preview tables = %+v", pr.Tables)
	}
}

// Spec §9: apply runs EXPLAIN validation for every selected query def against
// the selected local datasource — all-or-nothing, no def is created when
// validation fails. "dead" is unreachable, so every query fails at connect.
func TestMetaTransferQueryApplyExplainValidation(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "dead", Host: "x", Port: 1,
		DBName: "x", Username: "x", Password: "x", SSLMode: "disable"}); err != nil {
		t.Fatal(err)
	}
	bundle := func(sql string) string {
		b, _ := json.Marshal(map[string]any{
			"format": "ku-crud-meta", "version": 1,
			"groups": []string{}, "datasources": []map[string]any{},
			"tables": []map[string]any{{
				"datasourceRef": "dead", "schema": "", "table": "", "label": "Q",
				"keyColumns": []string{"n"}, "pageSize": 20, "sourceType": "query",
				"querySql": sql,
				"columns": []map[string]any{{"name": "n", "label": "N", "fieldType": "text",
					"editable": false, "required": false, "visible": true, "searchable": true,
					"sortable": true, "position": 0}},
			}},
		})
		return string(b)
	}
	sel := `{"datasources":[],"tables":[{"ref":"dead/query/Q","mode":"overwrite"}],"groups":false}`
	for _, q := range []string{
		"SELECT nope FROM nothing_qv",               // valid syntax, unplannable
		"WITH x AS (SELECT 1 AS n) SELECT * FROM x", // valid-looking; dead ds fails at connect
	} {
		resp := applyMeta(t, s, c, bundle(q), sel)
		if resp.Code != 400 || !strings.Contains(resp.Body.String(), "META_IMPORT_INVALID") {
			t.Fatalf("apply %q = %d %s", q, resp.Code, resp.Body)
		}
		if defs, _ := s.store.ListTableDefs(); len(defs) != 0 {
			t.Fatalf("apply %q must not create defs: %+v", q, defs)
		}
	}
}

// Live variant: with a reachable datasource the EXPLAIN really plans the
// query — unknown relations are rejected, valid queries import.
func TestMetaTransferQueryApplyExplainLive(t *testing.T) {
	if os.Getenv("KUCRUD_TEST_PG") == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	s := newTestServer(t)
	c := login(s)
	seedLive(t, s) // ds 1 "dead", ds 2 "live" (raw conn string) + customers def 1

	bundle := func(sql string) string {
		b, _ := json.Marshal(map[string]any{
			"format": "ku-crud-meta", "version": 1,
			"groups": []string{}, "datasources": []map[string]any{},
			"tables": []map[string]any{{
				"datasourceRef": "live", "schema": "", "table": "", "label": "Q",
				"keyColumns": []string{"n"}, "pageSize": 20, "sourceType": "query",
				"querySql": sql,
				"columns": []map[string]any{{"name": "n", "label": "N", "fieldType": "text",
					"editable": false, "required": false, "visible": true, "searchable": true,
					"sortable": true, "position": 0}},
			}},
		})
		return string(b)
	}
	sel := `{"datasources":[],"tables":[{"ref":"live/query/Q","mode":"overwrite"}],"groups":false}`

	resp := applyMeta(t, s, c, bundle("SELECT nope FROM nothing_qv"), sel)
	if resp.Code != 400 || !strings.Contains(resp.Body.String(), "META_IMPORT_INVALID") {
		t.Fatalf("unplannable apply = %d %s", resp.Code, resp.Body)
	}
	if defs, _ := s.store.ListTableDefs(); len(defs) != 1 { // only the seeded customers def
		t.Fatalf("unplannable apply must not create defs: %+v", defs)
	}

	resp = applyMeta(t, s, c, bundle("SELECT name AS n FROM customers"), sel)
	if resp.Code != 200 {
		t.Fatalf("valid apply = %d %s", resp.Code, resp.Body)
	}
	def, cols, err := s.store.GetTableDef(2)
	if err != nil || def == nil || def.SourceType != "query" || def.QuerySQL != "SELECT name AS n FROM customers" {
		t.Fatalf("imported def = %+v %v", def, err)
	}
	if len(cols) != 1 || cols[0].Name != "n" {
		t.Fatalf("imported cols = %+v", cols)
	}
}
