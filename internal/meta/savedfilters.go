package meta

import (
	"database/sql"
	"errors"
)

// ErrFilterTaken marks a duplicate saved-filter name (per user+table).
var ErrFilterTaken = errors.New("saved filter name already taken")

type SavedFilter struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"-"`
	TableDefID int64  `json:"-"`
	Name       string `json:"name"`
	Filters    string `json:"filters"`
	CreatedAt  string `json:"createdAt"`
}

func (s *Store) ListSavedFilters(userID, tableDefID int64) ([]SavedFilter, error) {
	rows, err := s.db.Query(`SELECT id,user_id,table_def_id,name,filters,created_at
		FROM saved_filters WHERE user_id=? AND table_def_id=? ORDER BY created_at,id`, userID, tableDefID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SavedFilter
	for rows.Next() {
		var sf SavedFilter
		if err := rows.Scan(&sf.ID, &sf.UserID, &sf.TableDefID, &sf.Name, &sf.Filters, &sf.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sf)
	}
	return out, rows.Err()
}

func (s *Store) CreateSavedFilter(userID, tableDefID int64, name, filters string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO saved_filters(user_id,table_def_id,name,filters) VALUES(?,?,?,?)`,
		userID, tableDefID, name, filters)
	if err != nil {
		return 0, ErrFilterTaken
	}
	id, _ := res.LastInsertId()
	return id, nil
}

func (s *Store) GetSavedFilter(id int64) (*SavedFilter, error) {
	var sf SavedFilter
	err := s.db.QueryRow(`SELECT id,user_id,table_def_id,name,filters,created_at FROM saved_filters WHERE id=?`, id).
		Scan(&sf.ID, &sf.UserID, &sf.TableDefID, &sf.Name, &sf.Filters, &sf.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &sf, nil
}

func (s *Store) UpdateSavedFilter(id int64, name, filters string) error {
	res, err := s.db.Exec(`UPDATE saved_filters SET name=?,filters=? WHERE id=?`, name, filters, id)
	if err != nil {
		return ErrFilterTaken
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteSavedFilter(id int64) error {
	res, err := s.db.Exec(`DELETE FROM saved_filters WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
