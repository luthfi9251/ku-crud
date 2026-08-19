package main

import (
	"flag"
	"io/fs"
	"log/slog"
	"net/http"
	"os"

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

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	store, err := meta.Open(*data)
	if err != nil {
		slog.Error("metadata store failed", "err", err.Error())
		os.Exit(1)
	}
	srv, err := api.New(store)
	if err != nil {
		slog.Error("server init failed", "err", err.Error())
		os.Exit(1)
	}
	mux := srv.Routes()
	static, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		slog.Error("embedded SPA missing", "err", err.Error())
		os.Exit(1)
	}
	mux.Handle("/", http.FileServer(http.FS(static)))

	slog.Info("ku-crud listening", "addr", *addr)
	if err := http.ListenAndServe(*addr, srv.WithLogging(mux)); err != nil {
		slog.Error("server stopped", "err", err.Error())
		os.Exit(1)
	}
}
