package api

import (
	"encoding/json"
	"net/http/httptest"
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
			"querySql": "SELECT name AS n FROM customers",
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

	// apply with the label-based ref
	resp = applyMeta(t, s2, c2, string(bb),
		`{"datasources":[],"tables":[{"ref":"dead/query/Q","mode":"overwrite"}],"groups":false}`)
	if resp.Code != 200 {
		t.Fatalf("apply = %d %s", resp.Code, resp.Body)
	}
	def, cols, err := s2.store.GetTableDef(1)
	if err != nil || def == nil || def.SourceType != "query" || def.QuerySQL != "SELECT name AS n FROM customers" {
		t.Fatalf("imported def = %+v %v", def, err)
	}
	if len(cols) != 1 || cols[0].Name != "n" {
		t.Fatalf("imported cols = %+v", cols)
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
