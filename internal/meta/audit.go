package meta

import (
	"database/sql"
	"encoding/json"
)

type AuditEntry struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"userId"`
	TableDefID int64  `json:"tableDefId"`
	Action     string `json:"action"`
	RowPK      string `json:"rowPk"`
	// json.RawMessage (= []byte) so API responses embed raw JSON objects,
	// not base64 — plain []byte would fail the raw-JSON contract.
	OldValues json.RawMessage `json:"oldValues"`
	NewValues json.RawMessage `json:"newValues"`
	CreatedAt string          `json:"createdAt"`
}

type AuditFilter struct {
	TableDefID, UserID int64
	Action             string
	Limit, Offset      int
}

func (s *Store) InsertAudit(e *AuditEntry) error {
	var oldV, newV any
	if len(e.OldValues) > 0 {
		oldV = string(e.OldValues)
	}
	if len(e.NewValues) > 0 {
		newV = string(e.NewValues)
	}
	_, err := s.db.Exec(`INSERT INTO audit(user_id,table_def_id,action,row_pk,old_values,new_values)
		VALUES(?,?,?,?,?,?)`, e.UserID, e.TableDefID, e.Action, e.RowPK, oldV, newV)
	return err
}

func (s *Store) ListAudit(f AuditFilter) ([]AuditEntry, int, error) {
	where := `WHERE 1=1`
	args := []any{}
	if f.TableDefID > 0 {
		where += ` AND table_def_id=?`
		args = append(args, f.TableDefID)
	}
	if f.UserID > 0 {
		where += ` AND user_id=?`
		args = append(args, f.UserID)
	}
	if f.Action != "" {
		where += ` AND action=?`
		args = append(args, f.Action)
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM audit `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT id,user_id,table_def_id,action,row_pk,old_values,new_values,created_at
		FROM audit ` + where + ` ORDER BY id DESC LIMIT ? OFFSET ?`
	args = append(args, f.Limit, f.Offset)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var oldV, newV sql.NullString
		if err := rows.Scan(&e.ID, &e.UserID, &e.TableDefID, &e.Action, &e.RowPK,
			&oldV, &newV, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		if oldV.Valid {
			e.OldValues = []byte(oldV.String)
		}
		if newV.Valid {
			e.NewValues = []byte(newV.String)
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}
