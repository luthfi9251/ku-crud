// Package platformhooks hosts the platform's own hook implementations —
// built-in automation the engine stays agnostic of. The audit trail
// reproduces the pre-extraction auditBestEffort semantics exactly: it
// runs synchronously inside the write request (before outbox enqueue,
// never failing it), records the acting user's id, the definition, the
// action (INSERT/UPDATE/DELETE), the audit-shaped row key and the
// old/new row values as raw JSON.
package platformhooks

import (
	"encoding/json"
	"log/slog"

	corehooks "github.com/luthfi9251/ku-crud/core/hooks"
	"ku-crud/internal/meta"
)

// mustJSON marshals a payload map; nil stays absent so the column reads
// SQL NULL (the old auditBestEffort behavior).
func mustJSON(v map[string]any) []byte {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("null")
	}
	return b
}

// Audit writes one audit entry for a completed write event. It backs
// the engine's optional SyncAfterHooks slot: the write path (rows,
// bulk delete, m2m junction links, CSV import) calls it inside the
// request, exactly where the pre-extraction handlers called
// auditBestEffort. rowKey is the audit row_pk as computed by the write
// path ("" where no single row key applies: creates, junction link
// changes, import). Best-effort: a failed audit write logs a warning
// and the request still succeeds.
func Audit(store *meta.Store, defID, userID int64, ev corehooks.Event, row corehooks.RowPayload, rowKey string) {
	var action string
	var oldB, newB []byte
	switch ev {
	case corehooks.AfterCreate:
		action, newB = "INSERT", mustJSON(row.Values)
	case corehooks.AfterUpdate:
		action, oldB, newB = "UPDATE", mustJSON(row.Old), mustJSON(row.Values)
	case corehooks.AfterDelete:
		action, oldB = "DELETE", mustJSON(row.Old)
	default:
		return
	}
	if err := store.InsertAudit(&meta.AuditEntry{
		UserID: userID, TableDefID: defID, Action: action, RowPK: rowKey,
		OldValues: oldB, NewValues: newB,
	}); err != nil {
		slog.Warn("audit write failed", "def", defID, "action", action,
			"rowPk", rowKey, "err", err.Error())
	}
}
