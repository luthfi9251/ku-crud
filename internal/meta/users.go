package meta

import (
	"database/sql"

	"golang.org/x/crypto/bcrypt"
)

func (s *Store) CreateUser(username, password string) error {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO users(username,password_hash) VALUES(?,?)`, username, string(h))
	return err
}

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) VerifyUser(username, password string) (bool, error) {
	var h string
	err := s.db.QueryRow(`SELECT password_hash FROM users WHERE username=?`, username).Scan(&h)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return bcrypt.CompareHashAndPassword([]byte(h), []byte(password)) == nil, nil
}

// UserID resolves a session username to its numeric id (re-checked per request).
func (s *Store) UserID(username string) (int64, bool, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM users WHERE username=?`, username).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}
