package main

import (
	"flag"
	"log"
	"net/http"

	"ku-crud/internal/api"
	"ku-crud/internal/meta"
)

// Dev entry: API only, no embedded SPA. Run the frontend separately:
//
//	cd web && npm run dev    # vite on :5173, proxies /api -> localhost:8080
//
// The production single binary (with embedded frontend) is built from repo root.
func main() {
	addr := flag.String("addr", ":8080", "listen address")
	data := flag.String("data", "ku-crud.db", "sqlite metadata path")
	flag.Parse()

	store, err := meta.Open(*data)
	if err != nil {
		log.Fatal(err)
	}
	srv := api.New(store)
	log.Printf("ku-crud dev API on %s — frontend: cd web && npm run dev → http://localhost:5173", *addr)
	log.Fatal(http.ListenAndServe(*addr, srv.Routes()))
}
