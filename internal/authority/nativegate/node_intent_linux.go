//go:build linux

package nativegate

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// NodeIntentBinding is a same-owner RAM proof for an already exclusively created
// node intent. It grants no recovery/adoption authority and carries no path.
// The caller retains the created descriptor until settlement or failure cleanup.
type NodeIntentBinding struct {
	lease     *Lease
	intent    *os.File
	info      os.FileInfo
	proofInfo os.FileInfo
	proof     []byte
}

func (l *Lease) BindNodeIntent(created *os.File) (*NodeIntentBinding, error) {
	if l == nil || created == nil {
		return nil, ErrNotOwner
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.owned || l.released || l.verifyNodeOwner() != nil {
		return nil, ErrNotOwner
	}
	info, err := created.Stat()
	if err != nil {
		return nil, ErrNotOwner
	}
	b := &NodeIntentBinding{lease: l, intent: created, info: info}
	content, stat, err := b.intentIdentity()
	if err != nil {
		return nil, err
	}
	b.proof = []byte(fmt.Sprintf("v1 %x %d %d %d %x\n", sha256.Sum256([]byte(l.record)), stat.Dev, stat.Ino, info.Size(), sha256.Sum256(content)))
	if len(b.proof) > 256 {
		return nil, ErrNotOwner
	}
	path := filepath.Join(l.root, "operation.lock.d", "node-intent")
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotOwner
	}
	// Link publishes the complete file atomically without overwriting an existing
	// proof. Any partial failure remains as evidence and prevents lease release.
	f, err := os.OpenFile(path+".tmp", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, ErrNotOwner
	}
	_, writeErr := f.Write(b.proof)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil || os.Link(path+".tmp", path) != nil || os.Remove(path+".tmp") != nil {
		return nil, ErrNotOwner
	}
	b.proofInfo, err = os.Lstat(path)
	if err != nil || b.verifyProof() != nil {
		return nil, ErrNotOwner
	}
	return b, nil
}

func (l *Lease) verifyNodeOwner() error {
	if protectedRoot(l.root) != nil {
		return ErrNotOwner
	}
	r, err := readOwner(l.root)
	if err != nil || r != l.record || strings.Fields(r)[5] != string(ConfigChange) {
		return ErrNotOwner
	}
	if _, err := Join(l.root, l.token); err != nil {
		return ErrNotOwner
	}
	return nil
}

func (b *NodeIntentBinding) intentIdentity() ([]byte, *syscall.Stat_t, error) {
	info, err := b.intent.Stat()
	if err != nil || !os.SameFile(info, b.info) || info.Mode() != 0600 || info.Size() != int64(len("node-operation-pending\n")) {
		return nil, nil, ErrNotOwner
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != 0 || st.Nlink != 1 {
		return nil, nil, ErrNotOwner
	}
	pathInfo, err := os.Lstat(b.intent.Name())
	if err != nil || !os.SameFile(info, pathInfo) || pathInfo.Mode() != 0600 {
		return nil, nil, ErrNotOwner
	}
	data := make([]byte, info.Size()+1)
	n, err := b.intent.ReadAt(data, 0)
	if err != nil && !errors.Is(err, io.EOF) || n != int(info.Size()) || string(data[:n]) != "node-operation-pending\n" {
		return nil, nil, ErrNotOwner
	}
	return data[:n], st, nil
}

func (b *NodeIntentBinding) verifyProof() error {
	if b.lease.verifyNodeOwner() != nil {
		return ErrNotOwner
	}
	path := filepath.Join(b.lease.root, "operation.lock.d", "node-intent")
	if protected(path, 0600, false) != nil {
		return ErrNotOwner
	}
	info, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, b.proofInfo) || info.Size() != int64(len(b.proof)) {
		return ErrNotOwner
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, b.proof) {
		return ErrNotOwner
	}
	return nil
}

func (b *NodeIntentBinding) Verify() error {
	b.lease.mu.Lock()
	defer b.lease.mu.Unlock()
	if b.lease.released || b.verifyProof() != nil {
		return ErrNotOwner
	}
	if _, _, err := b.intentIdentity(); err != nil {
		return err
	}
	return nil
}

// Clear is allowed only after the exact durable intent was removed and its
// directory synced by the transaction owner. Release never clears this proof.
func (b *NodeIntentBinding) Clear() error {
	b.lease.mu.Lock()
	defer b.lease.mu.Unlock()
	if b.lease.released || b.verifyProof() != nil {
		return ErrNotOwner
	}
	if _, err := os.Lstat(b.intent.Name()); !errors.Is(err, os.ErrNotExist) {
		return ErrNotOwner
	}
	return os.Remove(filepath.Join(b.lease.root, "operation.lock.d", "node-intent"))
}
