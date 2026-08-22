package meta

import (
	"database/sql"
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// UserCtx is the per-request user snapshot (auth context), joined with role.
type UserCtx struct {
	ID                int64
	Username          string
	RoleID            int64
	RoleName          string
	IsAdmin           bool
	ManageDatasources bool
	ManageTables      bool
	ViewAudit         bool
	ViewOutbox        bool
	IsFirst           bool
	Language          string
}

// UserWithRole is a users-listing row.
type UserWithRole struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	RoleID   int64  `json:"roleId"`
	RoleName string `json:"roleName"`
	Disabled bool   `json:"disabled"`
	IsFirst  bool   `json:"isFirst"`
}

func hash(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(h), err
}

// CreateUser creates an Admin user (CLI seeding / recovery path). The first
// user it creates on a fresh store is flagged is_first (setup-equivalent).
func (s *Store) CreateUser(username, password string) error {
	h, err := hash(password)
	if err != nil {
		return err
	}
	first := 0
	if n, err := s.CountUsers(); err == nil && n == 0 {
		first = 1
	}
	_, err = s.db.Exec(`INSERT INTO users(username,password_hash,role_id,is_first) VALUES(?,?,1,?)`,
		username, h, first)
	return err
}

func (s *Store) CreateUserWithRole(username, password string, roleID int64) error {
	h, err := hash(password)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO users(username,password_hash,role_id) VALUES(?,?,?)`,
		username, h, roleID)
	return err
}

// CreateFirstUser inserts the first user (Admin, is_first) only when the
// users table is empty. Single statement — no race window.
func (s *Store) CreateFirstUser(username, password string) (bool, error) {
	h, err := hash(password)
	if err != nil {
		return false, err
	}
	res, err := s.db.Exec(`INSERT INTO users(username,password_hash,role_id,is_first)
		SELECT ?,?,1,1 WHERE NOT EXISTS(SELECT 1 FROM users)`, username, string(h))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// VerifyUser reports whether username/password is a valid, enabled login.
func (s *Store) VerifyUser(username, password string) (bool, error) {
	var h string
	var disabled bool
	err := s.db.QueryRow(`SELECT password_hash,disabled FROM users WHERE username=?`, username).
		Scan(&h, &disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if disabled {
		return false, nil
	}
	return bcrypt.CompareHashAndPassword([]byte(h), []byte(password)) == nil, nil
}

// UserID resolves a session username to its numeric id (re-checked per request).
func (s *Store) UserID(username string) (int64, bool, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM users WHERE username=?`, username).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// GetUserContext resolves a session username to its full auth context.
// Disabled or missing users do not resolve.
func (s *Store) GetUserContext(username string) (UserCtx, bool, error) {
	var u UserCtx
	var disabled bool
	err := s.db.QueryRow(`SELECT u.id,u.username,u.role_id,r.name,r.is_admin,
		r.manage_datasources,r.manage_tables,r.view_audit,r.view_outbox,u.disabled,u.is_first,u.language
		FROM users u JOIN roles r ON r.id=u.role_id WHERE u.username=?`, username).
		Scan(&u.ID, &u.Username, &u.RoleID, &u.RoleName, &u.IsAdmin,
			&u.ManageDatasources, &u.ManageTables, &u.ViewAudit, &u.ViewOutbox,
			&disabled, &u.IsFirst, &u.Language)
	if errors.Is(err, sql.ErrNoRows) {
		return u, false, nil
	}
	if err != nil {
		return u, false, err
	}
	if disabled {
		return u, false, nil
	}
	return u, true, nil
}

// UpdateUserLanguage sets a user's UI language preference.
func (s *Store) UpdateUserLanguage(id int64, lang string) error {
	if lang != "en" && lang != "id" {
		return errors.New("language must be en or id")
	}
	_, err := s.db.Exec(`UPDATE users SET language=? WHERE id=?`, lang, id)
	return err
}

func (s *Store) ListUsers() ([]UserWithRole, error) {
	rows, err := s.db.Query(`SELECT u.id,u.username,u.role_id,r.name,u.disabled,u.is_first
		FROM users u JOIN roles r ON r.id=u.role_id ORDER BY u.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UserWithRole{}
	for rows.Next() {
		var u UserWithRole
		if err := rows.Scan(&u.ID, &u.Username, &u.RoleID, &u.RoleName, &u.Disabled, &u.IsFirst); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UpdateUser applies only the non-nil fields.
func (s *Store) UpdateUser(id int64, roleID *int64, disabled *bool, password *string) error {
	if roleID != nil {
		if _, err := s.db.Exec(`UPDATE users SET role_id=? WHERE id=?`, *roleID, id); err != nil {
			return err
		}
	}
	if disabled != nil {
		if _, err := s.db.Exec(`UPDATE users SET disabled=? WHERE id=?`, *disabled, id); err != nil {
			return err
		}
	}
	if password != nil {
		h, err := hash(*password)
		if err != nil {
			return err
		}
		if _, err := s.db.Exec(`UPDATE users SET password_hash=? WHERE id=?`, h, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeleteUser(id int64) error {
	res, err := s.db.Exec(`DELETE FROM users WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteUserByUsername(username string) error {
	res, err := s.db.Exec(`DELETE FROM users WHERE username=?`, username)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UserByID(id int64) (UserWithRole, error) {
	var u UserWithRole
	err := s.db.QueryRow(`SELECT u.id,u.username,u.role_id,r.name,u.disabled,u.is_first
		FROM users u JOIN roles r ON r.id=u.role_id WHERE u.id=?`, id).
		Scan(&u.ID, &u.Username, &u.RoleID, &u.RoleName, &u.Disabled, &u.IsFirst)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}
