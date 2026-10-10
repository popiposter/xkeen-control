package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// Cost 12 makes each hash take ~0.25 s, and far longer under -race. Behaviour
// does not depend on the cost, so package tests use the minimum.
var initialPasswordCost int

func TestMain(m *testing.M) {
	initialPasswordCost = passwordCost
	passwordCost = bcrypt.MinCost
	os.Exit(m.Run())
}

// The starting value is what production uses; it is recorded before TestMain
// lowers it, so changing the default fails here.
func TestProductionPasswordCost(t *testing.T) {
	if initialPasswordCost != 12 {
		t.Fatalf("production password cost = %d", initialPasswordCost)
	}
	passwordCost = initialPasswordCost
	defer func() { passwordCost = bcrypt.MinCost }()
	path := filepath.Join(t.TempDir(), "auth", "password.bcrypt")
	if err := SetPassword(path, []byte("synthetic-panel-password")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cost, err := bcrypt.Cost([]byte(strings.TrimSpace(string(raw)))); err != nil || cost != 12 {
		t.Fatalf("stored password cost = %d, %v", cost, err)
	}
}
