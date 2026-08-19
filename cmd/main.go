package main

import (
	"flag"
	"io/fs"
	"log"
	"net/http"

	"ku-crud/internal/api"
	"ku-crud/internal/meta"
	"ku-crud/web"
)

// Server entry: JSON API + embedded SPA. The go:embed directive lives in
// web/embed.go (embed cannot reach parent directories), so this file imports
// the SPA filesystem from there. Run: go run ./cmd/main.go — build: make build.
func main() {
	addr := flag.String("addr", ":8080", "listen address")
	data := flag.String("data", "ku-crud.db", "sqlite metadata path")
	flag.Parse()

	store, err := meta.Open(*data)
	if err != nil {
		log.Fatal(err)
	}
	mux := api.New(store).Routes()
	static, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("/", http.FileServer(http.FS(static)))

	log.Printf("ku-crud listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
