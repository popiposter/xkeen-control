package xkeen

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"github.com/popiposter/xkeen-control/internal/authority"
)

// The single receipt holds no console output, answers, parameters or session keys.
// It records a possible interrupted command; it never causes command replay.
type jobReceipt struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	State  string `json:"state"`
}

func NewPersistentJobs(binary string, lease *authority.Lease, path string) (*Jobs, error) {
	m := NewJobs(binary, lease)
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil || !parent.IsDir() || parent.Mode().Perm() != 0700 {
		return nil, ErrJob
	}
	m.receiptPath = path
	link, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil || !link.Mode().IsRegular() {
		return nil, ErrJob
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrJob
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(info, link) || info.Size() > 4096 || info.Mode().Perm() != 0600 {
		return nil, ErrJob
	}
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(data) > 4096 {
		return nil, ErrJob
	}
	var r jobReceipt
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&r) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return nil, ErrJob
	}
	id, idErr := hex.DecodeString(r.ID)
	known := false
	for _, spec := range CommandCatalog() {
		if spec.Action == r.Action {
			known = true
		}
	}
	if idErr != nil || len(id) != 16 || !known {
		return nil, ErrJob
	}
	if r.State == "running" {
		r.State = "unknown"
	}
	if r.State != "unknown" && r.State != "completed" && r.State != "failed" && r.State != "inspected" {
		return nil, ErrJob
	}
	m.job = &nativeJob{id: r.ID, action: r.Action, state: r.State}
	if r.State == "unknown" {
		m.Lease.Block()
	}
	return m, nil
}

func (m *Jobs) saveReceipt(j *nativeJob) error {
	if m.receiptPath == "" {
		return nil
	}
	data, err := json.Marshal(jobReceipt{ID: j.id, Action: j.action, State: j.state})
	if err != nil {
		return ErrJob
	}
	dir := filepath.Dir(m.receiptPath)
	f, err := os.CreateTemp(dir, ".native-job-*")
	if err != nil {
		return ErrJob
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil || os.Rename(f.Name(), m.receiptPath) != nil {
		return ErrJob
	}
	d, err := os.Open(dir)
	if err != nil {
		return ErrJob
	}
	defer d.Close()
	if d.Sync() != nil {
		return ErrJob
	}
	return nil
}
