package meta

import "errors"

// ErrGroupTaken marks a duplicate group name (UNIQUE constraint).
var ErrGroupTaken = errors.New("group name already taken")

type TableGroup struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

func (s *Store) ListGroups() ([]TableGroup, error) {
	rows, err := s.db.Query(`SELECT id,name,position FROM table_groups ORDER BY position,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TableGroup
	for rows.Next() {
		var g TableGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.Position); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) CreateGroup(name string) (int64, error) {
	var maxPos int
	s.db.QueryRow(`SELECT COALESCE(MAX(position),-1) FROM table_groups`).Scan(&maxPos)
	res, err := s.db.Exec(`INSERT INTO table_groups(name,position) VALUES(?,?)`, name, maxPos+1)
	if err != nil {
		return 0, ErrGroupTaken
	}
	id, _ := res.LastInsertId()
	return id, nil
}

func (s *Store) RenameGroup(id int64, name string) error {
	res, err := s.db.Exec(`UPDATE table_groups SET name=? WHERE id=?`, name, id)
	if err != nil {
		return ErrGroupTaken
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MoveGroup swaps positions with the neighboring group (dir -1 up, +1 down).
// Moving past the end is a no-op, not an error.
func (s *Store) MoveGroup(id int64, dir int) error {
	var ids []int64
	rows, err := s.db.Query(`SELECT id FROM table_groups ORDER BY position,id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var gid int64
		rows.Scan(&gid)
		ids = append(ids, gid)
	}
	rows.Close()
	idx := -1
	for i, gid := range ids {
		if gid == id {
			idx = i
		}
	}
	if idx < 0 {
		return ErrNotFound
	}
	swap := idx + dir
	if swap < 0 || swap >= len(ids) {
		return nil
	}
	s.db.Exec(`UPDATE table_groups SET position=? WHERE id=?`, swap, id)
	s.db.Exec(`UPDATE table_groups SET position=? WHERE id=?`, idx, ids[swap])
	return nil
}

// DeleteGroup removes the group; member tables fall back to ungrouped.
// Tables themselves are never deleted by group operations.
func (s *Store) DeleteGroup(id int64) error {
	if _, err := s.db.Exec(`UPDATE table_defs SET group_id=NULL WHERE group_id=?`, id); err != nil {
		return err
	}
	res, err := s.db.Exec(`DELETE FROM table_groups WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetTableGroup assigns (groupID>0) or clears (0) a table's group.
func (s *Store) SetTableGroup(defID, groupID int64) error {
	var gid any
	if groupID > 0 {
		gid = groupID
	}
	res, err := s.db.Exec(`UPDATE table_defs SET group_id=? WHERE id=?`, gid, defID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
