package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
)

//go:embed all:web/dist
var dist embed.FS

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	})
	static, _ := fs.Sub(dist, "web/dist")
	mux.Handle("/", http.FileServer(http.FS(static)))

	log.Printf("ku-crud listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
