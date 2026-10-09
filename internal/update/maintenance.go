package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/popiposter/xkeen-control/internal/buildinfo"
	"github.com/popiposter/xkeen-control/internal/release"
	"github.com/popiposter/xkeen-control/internal/setup"
)

type InstalledInspection struct {
	Digest string         `json:"digest"`
	Build  buildinfo.Info `json:"build"`
}

// InspectInstalled binds the complete fixed installed tuple. It executes no
// lifecycle action and does not infer that a live operation has completed.
func InspectInstalled() (InstalledInspection, error) {
	if MutationReady() != nil {
		return InstalledInspection{}, ErrInspectionRequired
	}
	return inspectInstalledTuple()
}

func inspectInstalledTuple() (InstalledInspection, error) {
	var out InstalledInspection
	h := sha256.New()
	for _, p := range []string{"/opt/sbin/xkeen-control", "/opt/etc/init.d/S99xkeen-control", DefaultHelperPath, DefaultMarkerPath} {
		sum, err := protectedHash(p)
		if err != nil {
			return out, err
		}
		info, err := os.Lstat(p)
		if err != nil {
			return out, err
		}
		fmt.Fprintf(h, "%s:%s:%d:%d:%d\n", p, sum, info.Mode(), info.Size(), info.ModTime().UnixNano())
	}
	f, err := os.Open(DefaultMarkerPath)
	if err != nil {
		return out, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 8193))
	if err != nil || len(b) > 8192 || json.Unmarshal(b, &out.Build) != nil || out.Build.Validate() != nil {
		return out, ErrInspectionRequired
	}
	out.Digest = hex.EncodeToString(h.Sum(nil))
	return out, nil
}

// ApplyMaintenance is an offline fixed-path delivery entrypoint for an installed
// generation whose helper cannot perform the repaired transaction. It never
// exposes a caller-selected executable or helper path through the API.
func ApplyMaintenance(ctx context.Context, expectedInstalledHash, version string) error {
	if len(expectedInstalledHash) != 64 || release.ValidateVersion(version) != nil {
		return ErrInspectionRequired
	}
	if _, err := hex.DecodeString(expectedInstalledHash); err != nil {
		return ErrInspectionRequired
	}
	lock, err := setup.MaintenanceFile()
	if err != nil {
		return err
	}
	defer lock.Close()
	if MutationReady() != nil {
		return ErrInspectionRequired
	}
	if err := maintenanceConsumersAbsent(); err != nil {
		return err
	}
	old, err := InspectInstalled()
	if err != nil || old.Digest != expectedInstalledHash {
		return ErrInspectionRequired
	}
	previous := old.Build
	child, cancel := context.WithTimeout(ctx, 10*time.Second)
	output, err := exec.CommandContext(child, "/opt/sbin/xkeen-control", "version", "--json").Output()
	cancel()
	var actual buildinfo.Info
	if err != nil || json.Unmarshal(output, &actual) != nil || actual != previous {
		return ErrInspectionRequired
	}
	client := release.NewClient()
	candidate, err := client.FetchCandidate(ctx, "stable", version)
	if err != nil {
		return err
	}
	current := buildinfo.Current()
	if current.Version != candidate.Manifest.Version || current.SourceCommit != candidate.Manifest.SourceCommit || current.Channel != candidate.Manifest.Channel {
		return ErrInspectionRequired
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if !strings.HasPrefix(self, "/tmp/") || !protectedDirectory(filepath.Dir(self)) {
		return ErrInspectionRequired
	}
	parent, err := os.Lstat(filepath.Dir(self))
	if err != nil || parent.Mode().Perm() != 0700 {
		return ErrInspectionRequired
	}
	selfHash, err := protectedHash(self)
	if err != nil {
		return err
	}
	wantSelf := ""
	for _, a := range candidate.Manifest.Artifacts {
		if a.Name == release.BinaryArtifact(candidate.Manifest.Architecture) {
			wantSelf = a.SHA256
		}
	}
	if selfHash != wantSelf {
		return ErrInspectionRequired
	}
	manager := NewManager(Config{Current: previous, Client: client})
	if err = manager.reserve(candidate); err != nil {
		return err
	}
	if err = manager.stage(candidate); err != nil {
		return err
	}
	again, err := inspectInstalledTuple()
	if err != nil || again != old {
		return ErrInspectionRequired
	}
	// Open verified helper and execute that same descriptor, avoiding pathname
	// substitution between verification and interpreter startup.
	path := filepath.Join(DefaultCandidateDir, "xkeen-control-updater")
	if !protectedPath(path) {
		return ErrInspectionRequired
	}
	named, err := os.Lstat(path)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(named, info) || info.Size() > 1<<20 {
		return ErrInspectionRequired
	}
	sum := sha256.New()
	if _, err = io.Copy(sum, io.LimitReader(f, 1<<20+1)); err != nil {
		return err
	}
	want := ""
	for _, a := range candidate.Manifest.Artifacts {
		if a.Name == "xkeen-control-updater" {
			want = a.SHA256
		}
	}
	if hex.EncodeToString(sum.Sum(nil)) != want {
		return ErrInspectionRequired
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(after, info) || after.ModTime() != info.ModTime() || after.Size() != info.Size() {
		return ErrInspectionRequired
	}
	if _, err = f.Seek(0, 0); err != nil {
		return err
	}
	// The helper inherits the same lock, downgrades it before daemon start, and
	// owns completion. The launcher performs no rollback or retry after handoff.
	command := exec.Command("/bin/sh", "/proc/self/fd/4", "install")
	command.ExtraFiles = []*os.File{lock, f}
	command.Env = append(os.Environ(), "XKEEN_CONTROL_UPDATE_LOCK_FD=3")
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err = command.Start(); err != nil {
		return err
	}
	lock.Close()
	return command.Wait()
}

func maintenanceConsumersAbsent() error {
	entries, err := os.ReadDir("/proc")
	if err != nil || len(entries) > 65536 {
		return ErrInspectionRequired
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.Trim(entry.Name(), "0123456789") != "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || len(b) > 1<<20 {
			return ErrInspectionRequired
		}
		args := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
		if len(args) == 1 && args[0] == "/opt/sbin/xkeen-control" {
			return ErrInspectionRequired
		}
		for _, arg := range args {
			if arg == DefaultHelperPath {
				return ErrInspectionRequired
			}
		}
	}
	return nil
}

func protectedHash(path string) (string, error) {
	if !protectedPath(path) {
		return "", ErrInspectionRequired
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return "", errors.New("update file identity unavailable")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	current, err := f.Stat()
	if err != nil || !os.SameFile(info, current) || current.Size() > 64<<20 {
		return "", ErrInspectionRequired
	}
	h := sha256.New()
	if _, err = io.Copy(h, io.LimitReader(f, 64<<20+1)); err != nil {
		return "", err
	}
	after, err := f.Stat()
	named, nameErr := os.Lstat(path)
	if err != nil || nameErr != nil || !os.SameFile(after, named) || !os.SameFile(current, after) || after.Size() != current.Size() || !after.ModTime().Equal(current.ModTime()) {
		return "", ErrInspectionRequired
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
