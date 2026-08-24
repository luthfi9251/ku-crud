package meta

import (
	"database/sql"
)

// OutboxEntry is one pending/failed after-hook execution. user_id/username
// snapshot the acting user — after-hooks run after the request is gone.
type OutboxEntry struct {
	ID          int64  `json:"id"`
	TableDefID  int64  `json:"tableDefId"`
	Event       string `json:"event"`
	HookName    string `json:"hookName"`
	Config      string `json:"config"`
	OldValues   string `json:"oldValues"`
	NewValues   string `json:"newValues"`
	UserID      int64  `json:"userId"`
	Username    string `json:"username"`
	Status      string `json:"status"`
	Attempts    int    `json:"attempts"`
	NextRetryAt string `json:"nextRetryAt"`
	LastError   string `json:"lastError"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

const outboxCols = `id,table_def_id,event,hook_name,config,old_values,new_values,user_id,username,status,attempts,next_retry_at,last_error,created_at,updated_at`

func scanOutbox(sc interface{ Scan(...any) error }) (OutboxEntry, error) {
	var e OutboxEntry
	var oldV, newV, next sql.NullString
	if err := sc.Scan(&e.ID, &e.TableDefID, &e.Event, &e.HookName, &e.Config, &oldV, &newV,
		&e.UserID, &e.Username, &e.Status, &e.Attempts, &next, &e.LastError, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return e, err
	}
	e.OldValues, e.NewValues, e.NextRetryAt = oldV.String, newV.String, next.String
	return e, nil
}

func (s *Store) EnqueueOutbox(e *OutboxEntry) error {
	res, err := s.db.Exec(`INSERT INTO hook_outbox(table_def_id,event,hook_name,config,old_values,new_values,user_id,username)
		VALUES(?,?,?,?,?,?,?,?)`,
		e.TableDefID, e.Event, e.HookName, e.Config, e.OldValues, e.NewValues, e.UserID, e.Username)
	if err != nil {
		return err
	}
	e.ID, _ = res.LastInsertId()
	return nil
}

// DueOutbox returns up to limit pending entries whose next_retry_at is due
// (or NULL), oldest first. next_retry_at is RFC3339 UTC, so string compare
// is chronological within that format.
func (s *Store) DueOutbox(now string, limit int) ([]OutboxEntry, error) {
	rows, err := s.db.Query(`SELECT `+outboxCols+` FROM hook_outbox
		WHERE status='pending' AND (next_retry_at IS NULL OR next_retry_at<=?)
		ORDER BY id LIMIT ?`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxEntry
	for rows.Next() {
		e, err := scanOutbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) MarkOutboxDone(id int64) error {
	_, err := s.db.Exec(`UPDATE hook_outbox SET status='done', last_error='', updated_at=datetime('now') WHERE id=?`, id)
	return err
}

// MarkOutboxFailed records a failure. Empty nextRetry marks the entry dead.
func (s *Store) MarkOutboxFailed(id int64, attempts int, nextRetry, lastErr string) error {
	var next any
	if nextRetry != "" {
		next = nextRetry
	}
	status := "pending"
	if nextRetry == "" {
		status = "dead"
	}
	_, err := s.db.Exec(`UPDATE hook_outbox SET status=?, attempts=?, next_retry_at=?, last_error=?, updated_at=datetime('now') WHERE id=?`,
		status, attempts, next, lastErr, id)
	return err
}

// RetryOutbox resets a dead/failed entry for immediate re-execution.
// Done entries are not retryable — their side effect already fired.
func (s *Store) RetryOutbox(id int64) error {
	res, err := s.db.Exec(`UPDATE hook_outbox SET status='pending', attempts=0, next_retry_at=NULL, updated_at=datetime('now') WHERE id=? AND status!='done'`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetOutbox(id int64) (*OutboxEntry, error) {
	e, err := scanOutbox(s.db.QueryRow(`SELECT `+outboxCols+` FROM hook_outbox WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Store) ListOutbox(status string, tableDefID int64, limit, offset int) ([]OutboxEntry, int, error) {
	where, args := `WHERE 1=1`, []any{}
	if status != "" {
		where += ` AND status=?`
		args = append(args, status)
	}
	if tableDefID > 0 {
		where += ` AND table_def_id=?`
		args = append(args, tableDefID)
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM hook_outbox `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT `+outboxCols+` FROM hook_outbox `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []OutboxEntry{}
	for rows.Next() {
		e, err := scanOutbox(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}
