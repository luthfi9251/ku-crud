// Command kucrud-template is a full-stack starter consuming the
// kucrud-core library: declare your tables in code, mount the app, ship
// the thin web UI. The host (this file) owns the HTTP server, the mux
// and authorization; the library owns the CRUD routes and the response
// shapes the web app consumes.
//
// Routes served (see core's httpapi for the full contract):
//
//	GET  /api/defs                       registered definitions
//	GET  /api/data/products/rows         list (search/sort/filter/paging)
//	POST /api/data/products/rows         create
//	GET|PUT|DELETE /api/data/products/rows/{key}   one row
//	... plus export, bulk-delete, fkoptions, m2moptions, import
package main

import (
	"database/sql"
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	kucrud "github.com/luthfi9251/kucrud-core"

	"kucrud-template/authstub"
)

func main() {
	addr := envOr("KUCRUD_ADDR", ":8080")

	app, err := newApp(connFromEnv(), authstub.Gate)
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()

	// The host owns the server and the mux: core routes sit next to
	// whatever else the host serves.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	mux.Handle("/api/", app)
	mountSPA(mux, "web/dist")

	log.Printf("kucrud-template listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

// connFromEnv builds the connection from KUCRUD_DB_DSN (a full
// postgres:// DSN) or the discrete KUCRUD_DB_HOST/PORT/NAME/USER/
// PASSWORD variables.
func connFromEnv() kucrud.Conn {
	if dsn := os.Getenv("KUCRUD_DB_DSN"); dsn != "" {
		return kucrud.Conn{Driver: "postgres", Raw: dsn}
	}
	port, _ := strconv.Atoi(envOr("KUCRUD_DB_PORT", "5432"))
	return kucrud.Conn{Driver: "postgres",
		Host: envOr("KUCRUD_DB_HOST", "localhost"), Port: port,
		DB: envOr("KUCRUD_DB_NAME", "ku"), User: envOr("KUCRUD_DB_USER", "ku"),
		Password: os.Getenv("KUCRUD_DB_PASSWORD")}
}

// newApp opens the connection, applies the example schema and registers
// the example resources. Registration introspects the database (types,
// keys, nullability) so a missing table fails here — startup config
// error, fail fast. Introspection supplies per-column defaults; the
// Overrides refine only what differs.
func newApp(c kucrud.Conn, gate kucrud.Gate) (*kucrud.App, error) {
	if err := applySchema(c); err != nil {
		return nil, err
	}
	app, err := kucrud.New(c, kucrud.WithGate(gate))
	if err != nil {
		return nil, err
	}

	// The example resource: one price override, one fk column, a default
	// sort. Everything else (labels, required-ness, searchability,
	// pagination) comes from introspection.
	app.CRUD("/api/data/products", kucrud.Def{
		Table: "products",
		Columns: []kucrud.Override{
			{Name: "price", Label: "Price", Format: `{"number":{"decimals":2}}`},
			{Name: "category_id", Label: "Category",
				FK: &kucrud.FK{Table: "categories", RefColumn: "id",
					DisplayColumns: []string{"name"}}},
		},
		DefaultSort: kucrud.Sort("created_at", kucrud.Desc),
	})
	// The fk target must also be registered (by def name) for fkoptions
	// and relation display to resolve.
	app.CRUD("/api/data/categories", kucrud.Def{Table: "categories"})

	return app, nil
}

//go:embed schema.sql
var schemaSQL string

// applySchema creates the example tables idempotently so the starter
// runs against a fresh database.
func applySchema(c kucrud.Conn) error {
	db, err := sql.Open("pgx", pgDSN(c))
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("example schema: %w", err)
	}
	return nil
}

// pgDSN renders the libpq keyword DSN for discrete Conn fields (Raw
// already is one).
func pgDSN(c kucrud.Conn) string {
	if c.Raw != "" {
		return c.Raw
	}
	return fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=disable",
		c.Host, c.Port, c.DB, c.User, c.Password)
}

// mountSPA serves the built web app (web/dist) when present, falling
// back to index.html for client-side routes. Build it with
// `npm install && npm run build` inside web/; without it the API still
// runs standalone.
func mountSPA(mux *http.ServeMux, dir string) {
	if _, err := os.Stat(dir); err != nil {
		log.Printf("serving API only: %s not found (build the web app: cd web && npm install && npm run build)", dir)
		return
	}
	files := os.DirFS(dir)
	fileServer := http.FileServerFS(files)
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := strings.TrimPrefix(r.URL.Path, "/"); p != "" {
			if f, err := files.Open(p); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		r.URL.Path = "/" // SPA fallback for client-side routes
		fileServer.ServeHTTP(w, r)
	}))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
