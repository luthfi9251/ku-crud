package main

import (
	"context"
	"flag"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/luthfi9251/ku-crud/core/hooks"
	_ "ku-crud/hooks" // compiled-in hooks register via registry_gen init()
	"ku-crud/internal/api"
	kuhooks "ku-crud/internal/hooks"
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
	srv, err := api.New(store, hooks.Default)
	if err != nil {
		slog.Error("server init failed", "err", err.Error())
		os.Exit(1)
	}

	worker := &kuhooks.Worker{Store: store, Reg: hooks.Default, Logger: slog.Default()}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go worker.Run(ctx)

	mux := srv.Routes()
	static, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		slog.Error("embedded SPA missing", "err", err.Error())
		os.Exit(1)
	}
	fileServer := http.FileServer(http.FS(static))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if f, err := static.Open(path); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" {
			http.NotFound(w, r)
			return
		}
		indexBytes, err := fs.ReadFile(static, "index.html")
		if err != nil {
			http.Error(w, "index.html not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexBytes)
	})

	slog.Info("ku-crud listening", "addr", *addr)
	httpSrv := &http.Server{Addr: *addr, Handler: srv.WithLogging(mux)}
	go func() {
		<-ctx.Done()
		httpSrv.Close()
	}()
	if err := httpSrv.ListenAndServe(); err != http.ErrServerClosed {
		slog.Error("server stopped", "err", err.Error())
		os.Exit(1)
	}
}
