// Package authstub ships the template's authorization stub: a deny-all
// Gate so the starter never deploys open CRUD endpoints by accident.
package authstub

import (
	"errors"
	"net/http"

	kucrud "github.com/luthfi9251/ku-crud/core"
)

// TODO: wire host auth — replace Gate with your real authorization
// (session/JWT check, per-table RBAC, ...). It is the single Gate slot:
// return nil to allow, non-nil to reject with 403. Until then every
// operation on every registered resource answers 403.
var Gate kucrud.Gate = func(r *http.Request, op kucrud.Op, table string) error {
	return errors.New("auth not configured: wire host auth in authstub/middleware.go")
}
