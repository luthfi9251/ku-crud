package api

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"

	"ku-crud/internal/meta"
)

func seedQueryLive(t *testing.T, s *Server) string {
	t.Helper()
	cs := os.Getenv("KUCRUD_TEST_PG")
	if cs == "" {
		t.Skip("KUCRUD_TEST_PG not set")
	}
	seedLive(t, s) // ds id 2 "live" + customers fixture (def 1)
	def := &meta.TableDef{DatasourceID: 2, SourceType: "query",
		QuerySQL: "SELECT name AS customer_name, balance FROM customers",
		Label:    "Customer names", KeyColumns: []string{"customer_name"}, PageSize: 2}
	cols := []meta.ColumnDef{
		{Name: "customer_name", Label: "Name", FieldType: "text", Visible: true,
			Searchable: true, Sortable: true, Position: 0},
		{Name: "balance", Label: "Balance", FieldType: "number", Visible: true,
			Sortable: true, Position: 1},
	}
	if err := s.store.SaveTableDef(def, cols); err != nil {
		t.Fatal(err)
	}
	// query views have no table name; the /api/data address is the token
	return tdTok(s, def.ID)
}

func TestQueryRowsListFilterSort(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	tok := seedQueryLive(t, s)

	w := do(s, "GET", "/api/data/"+tok+"/rows", "", c)
	if w.Code != 200 {
		t.Fatalf("list = %d %s", w.Code, w.Body)
	}
	var res struct {
		Total int              `json:"total"`
		Rows  []map[string]any `json:"rows"`
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Total != 3 || len(res.Rows) != 2 { // pageSize 2
		t.Fatalf("list total=%d rows=%d", res.Total, len(res.Rows))
	}

	f := url.QueryEscape(`[{"column":"customer_name","op":"contains","values":["jo"]}]`)
	w = do(s, "GET", "/api/data/"+tok+"/rows?filters="+f+"&sort=balance&dir=DESC", "", c)
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Total != 2 || res.Rows[0]["customer_name"] != "jo" { // DESC: 10.50 > 2.00
		t.Fatalf("filter+sort = total %d rows %+v", res.Total, res.Rows)
	}

	w = do(s, "GET", "/api/data/"+tok+"/rows?search=ana", "", c)
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Total != 1 {
		t.Fatalf("search = %d", res.Total)
	}
}

func TestQueryRowsGetAndExport(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	tok := seedQueryLive(t, s)

	pk := rowKeyToken([]string{"jo"})
	w := do(s, "GET", "/api/data/"+tok+"/rows/"+pk, "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "customer_name") {
		t.Fatalf("row get = %d %s", w.Code, w.Body)
	}

	body, _ := exportCSV(t, s, tok, "", *c)
	if lines := strings.Split(strings.TrimSpace(body), "\n"); len(lines) != 4 {
		t.Fatalf("export lines = %d", len(lines))
	}
}
