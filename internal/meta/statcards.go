package meta

import (
	"database/sql"
	"errors"
)

// StatCard is one admin-defined dashboard aggregate card; Filters is the
// serialized grid-format ActiveFilter[] JSON.
type StatCard struct {
	ID         int64  `json:"id"`
	TableDefID int64  `json:"-"`
	Label      string `json:"label"`
	Func       string `json:"func"`
	Column     string `json:"column"`
	Filters    string `json:"filters"`
	Position   int    `json:"position"`
}

func (s *Store) ListStatCards() ([]StatCard, error) {
	rows, err := s.db.Query(`SELECT id,table_def_id,label,func,column_name,filters,position
		FROM stat_cards ORDER BY position,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StatCard
	for rows.Next() {
		var c StatCard
		if err := rows.Scan(&c.ID, &c.TableDefID, &c.Label, &c.Func, &c.Column, &c.Filters, &c.Position); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetStatCard(id int64) (*StatCard, error) {
	var c StatCard
	err := s.db.QueryRow(`SELECT id,table_def_id,label,func,column_name,filters,position
		FROM stat_cards WHERE id=?`, id).
		Scan(&c.ID, &c.TableDefID, &c.Label, &c.Func, &c.Column, &c.Filters, &c.Position)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) CreateStatCard(tableDefID int64, label, fn, column, filters string) (int64, error) {
	var maxPos sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(position) FROM stat_cards`).Scan(&maxPos); err != nil {
		return 0, err
	}
	res, err := s.db.Exec(`INSERT INTO stat_cards(table_def_id,label,func,column_name,filters,position)
		VALUES(?,?,?,?,?,?)`, tableDefID, label, fn, column, filters, maxPos.Int64+1)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

func (s *Store) UpdateStatCard(id int64, label, fn, column, filters string) error {
	res, err := s.db.Exec(`UPDATE stat_cards SET label=?,func=?,column_name=?,filters=? WHERE id=?`,
		label, fn, column, filters, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteStatCard(id int64) error {
	res, err := s.db.Exec(`DELETE FROM stat_cards WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MoveStatCard swaps positions with the neighboring card (up = smaller
// position). Moving past the end is an error.
func (s *Store) MoveStatCard(id int64, up bool) error {
	list, err := s.ListStatCards()
	if err != nil {
		return err
	}
	idx := -1
	for i := range list {
		if list[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotFound
	}
	swap := idx - 1
	if !up {
		swap = idx + 1
	}
	if swap < 0 || swap >= len(list) {
		return errors.New("no neighbor to swap with")
	}
	a, b := list[idx], list[swap]
	if _, err := s.db.Exec(`UPDATE stat_cards SET position=? WHERE id=?`, b.Position, a.ID); err != nil {
		return err
	}
	if _, err := s.db.Exec(`UPDATE stat_cards SET position=? WHERE id=?`, a.Position, b.ID); err != nil {
		return err
	}
	return nil
}
