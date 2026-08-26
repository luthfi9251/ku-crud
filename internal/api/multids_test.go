package api

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/luthfi9251/kucrud-core/engine"
	"ku-crud/internal/meta"
)

// seedMultiDS wires a cross-driver pair: PG gets customers(3) and MySQL gets
// orders whose customer_id values (1..3, NULL) align with customers.id via a
// metadata-only fk relation (no db-level constraint). Returns the customer
// and orders def tokens.
func seedMultiDS(t *testing.T, s *Server) (custTok, ordTok string) {
	t.Helper()
	pgCS := os.Getenv("KUCRUD_TEST_PG")
	myCS := os.Getenv("KUCRUD_TEST_MYSQL")
	if pgCS == "" || myCS == "" {
		t.Skip("KUCRUD_TEST_PG and KUCRUD_TEST_MYSQL required")
	}
	if err := seedPG(pgCS); err != nil {
		t.Fatalf("pg seed: %v", err)
	}
	mySchema, err := seedMySQL(myCS)
	if err != nil {
		t.Fatalf("mysql seed: %v", err)
	}
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "pg", Driver: "postgres", Host: "x",
		Port: 1, DBName: "x", Username: "x", Password: "x", SSLMode: "disable", Raw: pgCS}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "my", Driver: "mysql", Host: "x",
		Port: 1, DBName: "x", Username: "x", Password: "x", SSLMode: "disable", Raw: myCS}); err != nil {
		t.Fatal(err)
	}
	cust := &meta.TableDef{DatasourceID: 1, SchemaName: "public", TableName: "customers",
		Label: "Customers", KeyColumns: []string{"id"}, PageSize: 20}
	if err := s.store.SaveTableDef(cust, []meta.ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 0},
		{Name: "name", Label: "Name", FieldType: "text", Editable: true, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 1},
	}); err != nil {
		t.Fatal(err)
	}
	orders := &meta.TableDef{DatasourceID: 2, SchemaName: mySchema, TableName: "orders",
		Label: "Orders", KeyColumns: []string{"id"}, PageSize: 20}
	if err := s.store.SaveTableDef(orders, []meta.ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
			Visible: true, Searchable: true, Sortable: true, Position: 0},
		{Name: "note", Label: "Note", FieldType: "text", Editable: true,
			Visible: true, Searchable: true, Sortable: true, Position: 1},
		{Name: "customer_id", Label: "Customer", FieldType: "fk", BaseType: "number",
			FKTableDefID: cust.ID, FKRefColumn: "id", FKDisplayColumns: []string{"name"},
			Editable: true, Visible: true, Searchable: true, Sortable: true, Position: 2},
	}); err != nil {
		t.Fatal(err)
	}
	return cust.TableName, orders.TableName
}

func seedPG(cs string) error {
	db, err := sql.Open("pgx", cs)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;
		CREATE TABLE customers(id serial PRIMARY KEY, name varchar(80) NOT NULL);
		INSERT INTO customers(name) VALUES ('jo'),('joe'),('ana');`)
	return err
}

// seedMySQL creates the orders table (relation to PG customers is
// metadata-only — no db-level FK) and returns the current database name.
func seedMySQL(cs string) (string, error) {
	db, err := sql.Open("mysql", cs)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var schema string
	if err := db.QueryRow("SELECT DATABASE()").Scan(&schema); err != nil {
		return "", err
	}
	// single statements per Exec: the mysql driver rejects multi-statement Exec
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS orders`,
		`CREATE TABLE orders(id INT AUTO_INCREMENT PRIMARY KEY, note VARCHAR(80), customer_id INT)`,
		`INSERT INTO orders(note,customer_id) VALUES ('o1',1),('o2',2),('o3',NULL)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return "", err
		}
	}
	return schema, nil
}

func TestCrossDriverRelations(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	custTok, ordTok := seedMultiDS(t, s)

	// rels: MySQL orders list enriched from PG customers
	w := do(s, "GET", "/api/data/"+ordTok+"/rows", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"rels":{"customer_id"`) ||
		!strings.Contains(w.Body.String(), `"name":"jo"`) {
		t.Fatalf("cross rels = %d %s", w.Code, w.Body)
	}

	// fkoptions picker reads PG target from MySQL source def
	w = do(s, "GET", "/api/data/"+ordTok+"/fkoptions/customer_id?search=joe", "", c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"joe"`) {
		t.Fatalf("cross fkoptions = %d %s", w.Code, w.Body)
	}

	// create with dangling fk → 400 (target lookup crosses drivers)
	w = do(s, "POST", "/api/data/"+ordTok+"/rows", `{"note":"x","customer_id":999}`, c)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "referenced row not found") {
		t.Fatalf("cross dangling = %d %s", w.Code, w.Body)
	}

	// delete referenced PG customer via API → 409 (metadata protection, cross-driver)
	w = do(s, "DELETE", "/api/data/"+custTok+"/rows/"+engine.EncodeRowKey([]string{"1"}), "", c)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "Orders") {
		t.Fatalf("cross delete protection = %d %s", w.Code, w.Body)
	}
}

// seedMySQLFK creates parent_t/child_t with a real db-level FK and returns
// the current database name. child_t dropped first so re-runs don't fail.
func seedMySQLFK(cs string) (string, error) {
	db, err := sql.Open("mysql", cs)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var schema string
	if err := db.QueryRow("SELECT DATABASE()").Scan(&schema); err != nil {
		return "", err
	}
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS child_t`,
		`DROP TABLE IF EXISTS parent_t`,
		`CREATE TABLE parent_t(id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(20))`,
		`CREATE TABLE child_t(id INT AUTO_INCREMENT PRIMARY KEY, pid INT,
			FOREIGN KEY (pid) REFERENCES parent_t(id))`,
		`INSERT INTO parent_t(name) VALUES ('p1')`,
		`INSERT INTO child_t(pid) VALUES (1)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return "", err
		}
	}
	return schema, nil
}

func TestMySQLFKViolation409(t *testing.T) {
	s := newTestServer(t)
	c := login(s)
	myCS := os.Getenv("KUCRUD_TEST_MYSQL")
	pgCS := os.Getenv("KUCRUD_TEST_PG")
	if myCS == "" || pgCS == "" {
		t.Skip("KUCRUD_TEST_PG and KUCRUD_TEST_MYSQL required")
	}
	schema, err := seedMySQLFK(myCS)
	if err != nil {
		t.Skipf("mysql fk seed: %v", err)
	}
	if err := s.store.CreateDatasource(&meta.Datasource{Name: "my2", Driver: "mysql", Host: "x",
		Port: 1, DBName: "x", Username: "x", Password: "x", SSLMode: "disable", Raw: myCS}); err != nil {
		t.Fatal(err)
	}
	// parent def WITHOUT any metadata fk relation → protection relies on db 1451 mapping
	def := &meta.TableDef{DatasourceID: 1, SchemaName: schema, TableName: "parent_t",
		Label: "ParentT", KeyColumns: []string{"id"}, PageSize: 20}
	if err := s.store.SaveTableDef(def, []meta.ColumnDef{
		{Name: "id", Label: "ID", FieldType: "number", Editable: false, Required: true,
			Visible: true, Sortable: true, Position: 0},
	}); err != nil {
		t.Fatal(err)
	}
	w := do(s, "DELETE", "/api/data/"+def.TableName+"/rows/"+engine.EncodeRowKey([]string{"1"}), "", c)
	if w.Code != 409 {
		t.Fatalf("mysql fk violation = %d %s", w.Code, w.Body)
	}
}
