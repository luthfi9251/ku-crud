package meta

import (
	"database/sql"
	"errors"
)

var (
	ErrInUse     = errors.New("role is assigned to users")
	ErrImmutable = errors.New("builtin admin role is immutable")
)

type Role struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	IsAdmin           bool   `json:"isAdmin"`
	ManageDatasources bool   `json:"manageDatasources"`
	ManageTables      bool   `json:"manageTables"`
	ViewAudit         bool   `json:"viewAudit"`
	ViewOutbox        bool   `json:"viewOutbox"`
}

type TableGrant struct {
	TableDefID int64 `json:"tableDefId"`
	CanRead    bool  `json:"canRead"`
	CanCreate  bool  `json:"canCreate"`
	CanUpdate  bool  `json:"canUpdate"`
	CanDelete  bool  `json:"canDelete"`
}

type RoleWithGrants struct {
	Role
	Grants    []TableGrant `json:"tables"`
	UserCount int          `json:"userCount"`
}

func insertGrants(tx *sql.Tx, roleID int64, grants []TableGrant) error {
	for _, g := range grants {
		if _, err := tx.Exec(`INSERT INTO role_table_grants(role_id,table_def_id,can_read,can_create,can_update,can_delete)
			VALUES(?,?,?,?,?,?)`, roleID, g.TableDefID, g.CanRead, g.CanCreate, g.CanUpdate, g.CanDelete); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CreateRole(r *Role, grants []TableGrant) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	res, err := tx.Exec(`INSERT INTO roles(name,is_admin,manage_datasources,manage_tables,view_audit,view_outbox) VALUES(?,0,?,?,?,?)`,
		r.Name, r.ManageDatasources, r.ManageTables, r.ViewAudit, r.ViewOutbox)
	if err != nil {
		tx.Rollback()
		return err
	}
	r.ID, _ = res.LastInsertId()
	if err := insertGrants(tx, r.ID, grants); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// UpdateRole replaces name, the grant flags and the full grant set.
// The builtin admin role cannot be modified.
func (s *Store) UpdateRole(r *Role, grants []TableGrant) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	var isAdmin bool
	if err := tx.QueryRow(`SELECT is_admin FROM roles WHERE id=?`, r.ID).Scan(&isAdmin); err != nil {
		tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if isAdmin {
		tx.Rollback()
		return ErrImmutable
	}
	if _, err := tx.Exec(`UPDATE roles SET name=?,manage_datasources=?,manage_tables=?,view_audit=?,view_outbox=? WHERE id=?`,
		r.Name, r.ManageDatasources, r.ManageTables, r.ViewAudit, r.ViewOutbox, r.ID); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM role_table_grants WHERE role_id=?`, r.ID); err != nil {
		tx.Rollback()
		return err
	}
	if err := insertGrants(tx, r.ID, grants); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteRole(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	var isAdmin bool
	if err := tx.QueryRow(`SELECT is_admin FROM roles WHERE id=?`, id).Scan(&isAdmin); err != nil {
		tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if isAdmin {
		tx.Rollback()
		return ErrImmutable
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE role_id=?`, id).Scan(&n); err != nil {
		tx.Rollback()
		return err
	}
	if n > 0 {
		tx.Rollback()
		return ErrInUse
	}
	if _, err := tx.Exec(`DELETE FROM roles WHERE id=?`, id); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) GetRole(id int64) (*Role, []TableGrant, error) {
	r := &Role{}
	err := s.db.QueryRow(`SELECT id,name,is_admin,manage_datasources,manage_tables,view_audit,view_outbox FROM roles WHERE id=?`, id).
		Scan(&r.ID, &r.Name, &r.IsAdmin, &r.ManageDatasources, &r.ManageTables, &r.ViewAudit, &r.ViewOutbox)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	grants, err := s.roleGrants(id)
	return r, grants, err
}

func (s *Store) roleGrants(roleID int64) ([]TableGrant, error) {
	rows, err := s.db.Query(`SELECT table_def_id,can_read,can_create,can_update,can_delete
		FROM role_table_grants WHERE role_id=? ORDER BY table_def_id`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TableGrant{}
	for rows.Next() {
		var g TableGrant
		if err := rows.Scan(&g.TableDefID, &g.CanRead, &g.CanCreate, &g.CanUpdate, &g.CanDelete); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) ListRoles() ([]RoleWithGrants, error) {
	rows, err := s.db.Query(`SELECT r.id,r.name,r.is_admin,r.manage_datasources,r.manage_tables,r.view_audit,r.view_outbox,
		(SELECT COUNT(*) FROM users u WHERE u.role_id=r.id)
		FROM roles r ORDER BY r.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RoleWithGrants{}
	for rows.Next() {
		var r RoleWithGrants
		if err := rows.Scan(&r.ID, &r.Name, &r.IsAdmin, &r.ManageDatasources, &r.ManageTables, &r.ViewAudit, &r.ViewOutbox, &r.UserCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	rows.Close()
	for i := range out {
		g, err := s.roleGrants(out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Grants = g
	}
	return out, nil
}

// GrantsFor returns the (role, table) grant; ErrNotFound when none is stored.
func (s *Store) GrantsFor(roleID, tableDefID int64) (TableGrant, error) {
	var g TableGrant
	err := s.db.QueryRow(`SELECT table_def_id,can_read,can_create,can_update,can_delete
		FROM role_table_grants WHERE role_id=? AND table_def_id=?`, roleID, tableDefID).
		Scan(&g.TableDefID, &g.CanRead, &g.CanCreate, &g.CanUpdate, &g.CanDelete)
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrNotFound
	}
	return g, err
}
