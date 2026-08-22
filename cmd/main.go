package main

import (
	"context"
	"flag"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

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
	srv, err := api.New(store, kuhooks.Default)
	if err != nil {
		slog.Error("server init failed", "err", err.Error())
		os.Exit(1)
	}

	worker := &kuhooks.Worker{Store: store, Reg: kuhooks.Default, Logger: slog.Default()}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go worker.Run(ctx)

	mux := srv.Routes()
	static, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		slog.Error("embedded SPA missing", "err", err.Error())
		os.Exit(1)
	}
	mux.Handle("/", http.FileServer(http.FS(static)))

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
