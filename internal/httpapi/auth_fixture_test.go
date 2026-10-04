package httpapi

import (
	"os"
	"path/filepath"

	"golang.org/x/crypto/bcrypt"
)

// setHTTPTestPassword prepares route fixtures without repeating production-cost
// key expansion. Authentication still uses the real protected-file reader and
// bcrypt comparison; sessions, CSRF, reauthentication and rotation are unchanged.
// Production password creation is covered by the auth package and the explicit
// SetPassword call in TestServerAuthReadOnlyRoutesAndHealthBoundary.
func setHTTPTestPassword(path string, password []byte) error {
	hash, err := bcrypt.GenerateFromPassword(password, bcrypt.MinCost)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, hash, 0o600)
}
