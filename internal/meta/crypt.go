package meta

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

// Datasource passwords are encrypted at rest with AES-256-GCM. The key is a
// random 32 bytes stored (hex) in the settings table under "dsn_crypt_key".
// This removes plaintext credentials from casual reads/dumps of the SQLite
// file; it is hardening, not a security boundary — the key lives in the same
// file. Ciphertext format: "v1:" + base64(nonce ‖ ciphertext+tag).

const cryptPrefix = "v1:"

func (s *Store) cryptKey() ([]byte, error) {
	if v, ok, err := s.Setting("dsn_crypt_key"); err != nil {
		return nil, err
	} else if ok {
		return hex.DecodeString(v)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	if err := s.SetSetting("dsn_crypt_key", hex.EncodeToString(raw)); err != nil {
		return nil, err
	}
	return raw, nil
}

func encryptPassword(s *Store, plain string) (string, error) {
	key, err := s.cryptKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return cryptPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// decryptPassword returns the plaintext for "v1:"-prefixed values. Values
// without the prefix are returned unchanged (legacy plaintext passthrough),
// so old stores keep working until the migration pass rewrites them.
func decryptPassword(s *Store, enc string) (string, error) {
	if !strings.HasPrefix(enc, cryptPrefix) {
		return enc, nil
	}
	key, err := s.cryptKey()
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(enc, cryptPrefix))
	if err != nil {
		return "", fmt.Errorf("datasource password: bad encoding: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("datasource password: ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("datasource password: decrypt failed: %w", err)
	}
	return string(plain), nil
}

// encryptLegacyPasswords rewrites any pre-v1.3 plaintext password rows. It
// runs once after migrations on startup and is idempotent.
func (s *Store) encryptLegacyPasswords() error {
	rows, err := s.db.Query(`SELECT id,password FROM datasources`)
	if err != nil {
		return err
	}
	type row struct {
		id  int64
		pwd string
	}
	var legacy []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.pwd); err != nil {
			rows.Close()
			return err
		}
		if !strings.HasPrefix(r.pwd, cryptPrefix) {
			legacy = append(legacy, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range legacy {
		enc, err := encryptPassword(s, r.pwd)
		if err != nil {
			return err
		}
		if _, err := s.db.Exec(`UPDATE datasources SET password=? WHERE id=?`, enc, r.id); err != nil {
			return err
		}
	}
	return nil
}
