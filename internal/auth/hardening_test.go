package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestAttemptPressurePreservesEveryActiveLockout(t *testing.T) {
	now := time.Now()
	m := NewManager(Config{Now: func() time.Time { return now }})
	for i := 0; i < maxAttempts; i++ {
		m.attempts[fmt.Sprint(i)] = attempt{firstAt: now.Add(-time.Hour), lockedUntil: now.Add(time.Minute), failures: 5}
	}
	if _, _, err := m.Login("new-remote", "unused"); err != ErrLocked {
		t.Fatalf("login: %v", err)
	}
	if err := m.Reauthenticate("new-remote", "unused"); err != ErrLocked {
		t.Fatalf("reauth: %v", err)
	}
	if len(m.attempts) != maxAttempts {
		t.Fatal("attempt cap exceeded")
	}
	if _, exists := m.attempts["new-remote"]; exists {
		t.Fatal("pressure admitted new remote")
	}
	m.attempts["a"] = m.attempts["0"]
	delete(m.attempts, "0")
	m.attempts["a"] = attempt{firstAt: now.Add(-time.Minute)}
	m.attempts["b"] = attempt{firstAt: now.Add(-time.Minute)}
	delete(m.attempts, "1")
	if !m.recordFailure("new-remote", now) {
		t.Fatal("non-locked entry not displaced")
	}
	if _, exists := m.attempts["a"]; exists {
		t.Fatal("stable oldest tie-break not used")
	}
	if len(m.attempts) != maxAttempts {
		t.Fatal("wrong bounded cardinality")
	}
	// Expired failure windows and expired lockouts can now be pruned.
	now = now.Add(11 * time.Minute)
	if !m.recordFailure("later", now) || len(m.attempts) != 1 {
		t.Fatal("expired attempts not pruned")
	}
}

func TestConcurrentAttemptAdmissionStaysBounded(t *testing.T) {
	now := time.Now()
	m := NewManager(Config{LockoutAfter: 1})
	var workers sync.WaitGroup
	for i := 0; i < maxAttempts+64; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			m.recordFailure(fmt.Sprint(i), now)
		}(i)
	}
	workers.Wait()
	if len(m.attempts) != maxAttempts {
		t.Fatal("concurrent admission exceeded cap")
	}
	for _, value := range m.attempts {
		if !now.Before(value.lockedUntil) {
			t.Fatal("active lockout lost")
		}
	}
	if m.recordFailure("unseen", now) {
		t.Fatal("all-locked table admitted unseen remote")
	}
}

func TestSessionAdmissionPrunesAndDeterministicallyEvicts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth", "password.bcrypt")
	if err := SetPassword(path, []byte("synthetic-panel-password")); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	m := NewManager(Config{HashPath: path, Now: func() time.Time { return now }})
	for i := 0; i < maxSessions; i++ {
		m.sessions[fmt.Sprintf("%02d", i)] = session{expiresAt: now.Add(time.Hour)}
	}
	m.sessions["expired"] = session{expiresAt: now}
	if _, _, err := m.Login("local", "synthetic-panel-password"); err != nil {
		t.Fatal(err)
	}
	if len(m.sessions) != maxSessions {
		t.Fatal("session cap exceeded")
	}
	if _, exists := m.sessions["00"]; exists {
		t.Fatal("oldest tie-break not evicted")
	}
	if _, exists := m.sessions["expired"]; exists {
		t.Fatal("expired session retained")
	}
	now = now.Add(9 * time.Hour)
	if _, _, err := m.Login("local", "synthetic-panel-password"); err != nil {
		t.Fatal(err)
	}
	if len(m.sessions) != 1 {
		t.Fatal("expired session cleanup failed")
	}
}

func TestPasswordAuthorityUnsafeStatesFailClosedWithoutRepair(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux file authority boundary")
	}
	for _, kind := range []string{"symlink", "directory", "oversize", "hash-mode", "parent-mode", "hash-owner", "parent-owner", "parent-symlink", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "auth")
			path := filepath.Join(dir, "password.bcrypt")
			if err := SetPassword(path, []byte("synthetic-panel-password")); err != nil {
				t.Fatal(err)
			}
			must := func(err error) {
				if err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "symlink":
				must(os.Rename(path, path+".real"))
				must(os.Symlink(path+".real", path))
			case "directory":
				must(os.Remove(path))
				must(os.Mkdir(path, 0o700))
			case "oversize":
				must(os.WriteFile(path, make([]byte, 257), 0o600))
			case "hash-mode":
				must(os.Chmod(path, 0o644))
			case "parent-mode":
				must(os.Chmod(dir, 0o755))
			case "hash-owner":
				must(os.Chown(path, 1234, 1234))
			case "parent-owner":
				must(os.Chown(dir, 1234, 1234))
			case "parent-symlink":
				must(os.Rename(dir, dir+".real"))
				must(os.Symlink(dir+".real", dir))
			case "malformed":
				must(os.WriteFile(path, []byte("invalid hash"), 0o600))
			}
			before, err := os.Lstat(path)
			must(err)
			m := NewManager(Config{HashPath: path})
			if _, _, err := m.Login("local", "synthetic-panel-password"); err != ErrNotConfigured {
				t.Fatalf("login: %v", err)
			}
			if err := m.Reauthenticate("local", "synthetic-panel-password"); err != ErrNotConfigured {
				t.Fatalf("reauth: %v", err)
			}
			if m.CredentialState() != "unavailable" {
				t.Fatal("unsafe credential reported available")
			}
			after, err := os.Lstat(path)
			must(err)
			if !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() {
				t.Fatal("reader repaired unsafe state")
			}
		})
	}
}

func TestPasswordAuthorityReplacementRace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth", "password.bcrypt")
	if err := SetPassword(path, []byte("synthetic-panel-password")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"before-open", "after-open", "parent"} {
		t.Run(phase, func(t *testing.T) {
			_, err := readPasswordAuthorityWithOpen(path, func(path string) (*os.File, error) {
				replace := func() error {
					if phase == "parent" {
						return os.Chmod(filepath.Dir(path), 0o755)
					}
					if err := os.WriteFile(path+".new", data, 0o600); err != nil {
						return err
					}
					return os.Rename(path+".new", path)
				}
				if phase == "before-open" {
					if err := replace(); err != nil {
						return nil, err
					}
					return openPasswordAuthority(path)
				}
				f, err := openPasswordAuthority(path)
				if err != nil {
					return nil, err
				}
				if err := replace(); err != nil {
					f.Close()
					return nil, err
				}
				return f, nil
			})
			if err == nil {
				t.Fatal("authority race accepted")
			}
			if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
		})
	}
}
