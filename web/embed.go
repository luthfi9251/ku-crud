package web

import "embed"

// Dist holds the built SPA (web/dist). The placeholder index.html is committed
// so the module compiles without a frontend build. The embed directive lives
// here — next to web/dist — because go:embed cannot reach parent directories;
// cmd/main.go imports this package.
//
//go:embed all:dist
var Dist embed.FS
