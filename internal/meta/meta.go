package meta

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db   *sql.DB
	path string
}

var migrations = []string{`
CREATE TABLE settings(key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE users(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE datasources(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  host TEXT NOT NULL, port INTEGER NOT NULL,
  dbname TEXT NOT NULL, username TEXT NOT NULL,
  password TEXT NOT NULL, sslmode TEXT NOT NULL DEFAULT 'disable');
CREATE TABLE table_defs(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  datasource_id INTEGER NOT NULL REFERENCES datasources(id) ON DELETE CASCADE,
  schema_name TEXT NOT NULL, table_name TEXT NOT NULL,
  label TEXT NOT NULL, pk_column TEXT NOT NULL,
  page_size INTEGER NOT NULL DEFAULT 20,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE columns(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  table_def_id INTEGER NOT NULL REFERENCES table_defs(id) ON DELETE CASCADE,
  name TEXT NOT NULL, label TEXT NOT NULL,
  field_type TEXT NOT NULL, enum_options TEXT,
  editable INTEGER NOT NULL DEFAULT 1,
  required INTEGER NOT NULL DEFAULT 0,
  visible INTEGER NOT NULL DEFAULT 1,
  searchable INTEGER NOT NULL DEFAULT 1,
  sortable INTEGER NOT NULL DEFAULT 1,
  position INTEGER NOT NULL);
CREATE TABLE audit(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL, table_def_id INTEGER NOT NULL,
  action TEXT NOT NULL, row_pk TEXT NOT NULL,
  old_values TEXT, new_values TEXT,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP);
`,
	`ALTER TABLE datasources ADD COLUMN raw TEXT NOT NULL DEFAULT '';`,
	// v1.1: roles & per-table grants, user flags, composite key columns.
	// Every pre-existing user becomes Admin (id 1); pk_column migrates into
	// a key_columns JSON array. No FK on users.role_id: SQLite cannot ADD
	// COLUMN with REFERENCES and a non-NULL default; integrity is app-managed.
	`CREATE TABLE roles(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  is_admin INTEGER NOT NULL DEFAULT 0,
  platform_manage INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP);
INSERT INTO roles(id,name,is_admin,platform_manage) VALUES(1,'Admin',1,1);
ALTER TABLE users ADD COLUMN role_id INTEGER NOT NULL DEFAULT 1;
ALTER TABLE users ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN is_first INTEGER NOT NULL DEFAULT 0;
UPDATE users SET is_first=1 WHERE id=(SELECT MIN(id) FROM users);
CREATE TABLE role_table_grants(
  role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  table_def_id INTEGER NOT NULL REFERENCES table_defs(id) ON DELETE CASCADE,
  can_read INTEGER NOT NULL DEFAULT 0,
  can_create INTEGER NOT NULL DEFAULT 0,
  can_update INTEGER NOT NULL DEFAULT 0,
  can_delete INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(role_id,table_def_id));
ALTER TABLE table_defs ADD COLUMN key_columns TEXT NOT NULL DEFAULT '[]';
UPDATE table_defs SET key_columns=json_array(pk_column);
ALTER TABLE table_defs DROP COLUMN pk_column;`,
	// v1.2: FK relation columns. fk_table_def_id 0 = not an FK reference.
	`ALTER TABLE columns ADD COLUMN base_type TEXT NOT NULL DEFAULT '';
ALTER TABLE columns ADD COLUMN fk_table_def_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE columns ADD COLUMN fk_ref_column TEXT NOT NULL DEFAULT '';
ALTER TABLE columns ADD COLUMN fk_display_columns TEXT;`,
	// v1.2 phase 2: adapter driver per datasource ('postgres' | 'mysql' | future).
	`ALTER TABLE datasources ADD COLUMN driver TEXT NOT NULL DEFAULT 'postgres';`,
	// v1.3: default sort on table definitions + many-to-many virtual columns.
	`ALTER TABLE table_defs ADD COLUMN default_sort_col TEXT NOT NULL DEFAULT '';
ALTER TABLE table_defs ADD COLUMN default_sort_dir TEXT NOT NULL DEFAULT 'ASC';
ALTER TABLE columns ADD COLUMN m2m_junction_def_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE columns ADD COLUMN m2m_junction_src_col TEXT NOT NULL DEFAULT '';
ALTER TABLE columns ADD COLUMN m2m_junction_tgt_col TEXT NOT NULL DEFAULT '';
ALTER TABLE columns ADD COLUMN m2m_display_cols TEXT NOT NULL DEFAULT '';`,
	// v1.4: sidebar table groups + per-column validation rules.
	`CREATE TABLE table_groups (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	name     TEXT NOT NULL UNIQUE,
	position INTEGER NOT NULL DEFAULT 0
);
ALTER TABLE table_defs ADD COLUMN group_id INTEGER REFERENCES table_groups(id) ON DELETE SET NULL;
ALTER TABLE columns ADD COLUMN validations TEXT NOT NULL DEFAULT '';`,
	// v1.5: computed columns, per-column formatting, default views,
	// user language and saved filters.
	`ALTER TABLE columns ADD COLUMN formatting TEXT NOT NULL DEFAULT '';
ALTER TABLE columns ADD COLUMN is_computed INTEGER NOT NULL DEFAULT 0;
ALTER TABLE columns ADD COLUMN computed_formula TEXT NOT NULL DEFAULT '';
ALTER TABLE table_defs ADD COLUMN default_view TEXT NOT NULL DEFAULT 'grid';
ALTER TABLE table_defs ADD COLUMN view_config TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN language TEXT NOT NULL DEFAULT 'en';
CREATE TABLE saved_filters (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  table_def_id  INTEGER NOT NULL REFERENCES table_defs(id) ON DELETE CASCADE,
  name          TEXT NOT NULL,
  filters       TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE UNIQUE INDEX idx_saved_filters ON saved_filters(user_id, table_def_id, name);`,
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // modernc sqlite: single writer, avoids SQLITE_BUSY
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		return nil, err
	}
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	if err := s.encryptLegacyPasswords(); err != nil {
		return nil, fmt.Errorf("password encryption migration: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	// fresh database: settings table doesn't exist until migration 1 runs
	v := 0
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='settings'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		var txt string
		err := s.db.QueryRow(`SELECT value FROM settings WHERE key='schema_version'`).Scan(&txt)
		if err == sql.ErrNoRows {
			txt = "0"
		} else if err != nil {
			return err
		}
		if _, err := fmt.Sscan(txt, &v); err != nil {
			return fmt.Errorf("bad schema_version %q: %w", txt, err)
		}
	}
	for i := v; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(`INSERT INTO settings(key,value) VALUES('schema_version',?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value`, fmt.Sprint(i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Setting(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// SessionSecret returns the HMAC secret, generating a random one on first call.
func (s *Store) SessionSecret() ([]byte, error) {
	return s.secretSetting("session_secret")
}

// IDSecret returns the id-token secret, generating a random one on first call.
func (s *Store) IDSecret() ([]byte, error) {
	return s.secretSetting("id_secret")
}

func (s *Store) secretSetting(key string) ([]byte, error) {
	if v, ok, err := s.Setting(key); err != nil {
		return nil, err
	} else if ok {
		return []byte(v), nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	hx := hex.EncodeToString(b)
	if err := s.SetSetting(key, hx); err != nil {
		return nil, err
	}
	return []byte(hx), nil
}
