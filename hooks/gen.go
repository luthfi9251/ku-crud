// Package hooks holds developer-written automation functions. Add a plain
// function with the HookFunc signature, run `make dev` / `make build`
// (which regenerate registry_gen.go), restart — the hook appears in the
// table-definition editor. Hook name = Go function name.
package hooks

//go:generate go run ../cmd/hookgen
