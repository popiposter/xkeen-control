package xkeen

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/popiposter/xkeen-control/internal/authority"
)

const maxJobOutput = 256 << 10

var ErrJob = errors.New("native job unavailable")

// JobView is a PRIVATE session-bound console response, not a status projection.
// Completed means process exit, never verified tunnel health.
type JobView struct {
	ID          string `json:"id"`
	Action      string `json:"action"`
	State       string `json:"state"`
	Interactive bool   `json:"interactive"`
	ExitCode    *int   `json:"exitCode,omitempty"`
	Output      string `json:"output"` // base64 preserves partial UTF-8/ANSI bytes
	Cursor      int64  `json:"cursor"`
	Truncated   bool   `json:"truncated"`
}

type nativeJob struct {
	id, owner, action, state string
	interactive              bool
	output                   []byte
	base                     int64
	exit                     *int
	terminal                 *os.File
	command                  *exec.Cmd
	cancel                   context.CancelFunc
	lastActivity             time.Time
}

// Jobs retains just one bounded job. Closing a browser never cancels its process.
// Lease is shared with config/node actions; it excludes panel actions only.
type Jobs struct {
	Binary        string
	Lease         *authority.Lease
	mu            sync.Mutex
	job           *nativeJob
	idleTimeout   time.Duration
	receiptPath   string
	startTerminal func(*exec.Cmd, bool) (*os.File, error)
}

func NewJobs(binary string, lease *authority.Lease) *Jobs {
	if binary == "" {
		binary = "/opt/sbin/xkeen"
	}
	if lease == nil {
		lease = authority.NewLease()
	}
	return &Jobs{Binary: binary, Lease: lease, idleTimeout: 10 * time.Minute, startTerminal: startNativeTerminal}
}

func (m *Jobs) Start(owner string, r CommandRequest) (JobView, error) {
	if owner == "" {
		return JobView{}, ErrJob
	}
	spec, args, err := commandArguments(r)
	if err != nil {
		return JobView{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.job != nil && (m.job.state == "running" || m.job.state == "unknown") {
		return JobView{}, authority.ErrBusy
	}
	release, err := m.Lease.TryAcquire()
	if err != nil {
		return JobView{}, err
	}
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		release()
		return JobView{}, ErrJob
	}
	j := &nativeJob{id: hex.EncodeToString(id), owner: owner, action: r.Action, state: "running", interactive: spec.Interactive, lastActivity: time.Now()}
	if m.saveReceipt(j) != nil {
		m.Lease.Block()
		release()
		return JobView{}, ErrJob
	}
	ctx, cancel := context.WithTimeout(context.Background(), spec.limit)
	command := exec.Command(m.Binary, args...)
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "XKEEN_FOREGROUND=") && !strings.HasPrefix(e, "XKEEN_GATE_") && !strings.HasPrefix(e, "TERM=") {
			command.Env = append(command.Env, e)
		}
	}
	command.Env = append(command.Env, "XKEEN_FOREGROUND=1", "TERM=xterm-256color")
	terminal, err := m.startTerminal(command, spec.Interactive)
	if err != nil {
		cancel()
		// Terminal setup can fail after exec has already started native work.
		// Never turn that into a safely replayable startup refusal.
		j.state = "unknown"
		m.job = j
		_ = m.saveReceipt(j)
		m.Lease.Block()
		release()
		return JobView{}, ErrJob
	}
	j.terminal, j.command, j.cancel = terminal, command, cancel
	m.job = j
	if j.interactive {
		go m.watchIdle(ctx, j)
	}
	go m.run(ctx, j, release)
	return m.view(j, 0), nil
}

// Viewing or polling the console does not keep an unanswered native prompt alive.
// Only actual process output or successfully delivered input counts as activity.
func (m *Jobs) watchIdle(ctx context.Context, j *nativeJob) {
	timer := time.NewTimer(m.idleTimeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			m.mu.Lock()
			remaining := m.idleTimeout - time.Since(j.lastActivity)
			m.mu.Unlock()
			if remaining <= 0 {
				j.cancel()
				return
			}
			timer.Reset(remaining)
		}
	}
}

func (m *Jobs) run(ctx context.Context, j *nativeJob, release func()) {
	defer release()
	defer j.cancel()
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		buf := make([]byte, 4096)
		for {
			n, err := j.terminal.Read(buf)
			if n > 0 {
				m.mu.Lock()
				j.lastActivity = time.Now()
				j.output = append(j.output, buf[:n]...)
				if len(j.output) > maxJobOutput {
					drop := len(j.output) - maxJobOutput
					j.output = append([]byte(nil), j.output[drop:]...)
					j.base += int64(drop)
				}
				m.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	done := make(chan error, 1)
	go func() { done <- j.command.Wait() }()
	state := "completed"
	var err error
	select {
	case err = <-done:
		if ctx.Err() != nil {
			state = "unknown"
		} else if err != nil {
			state = "failed"
		}
	case <-ctx.Done():
		state = "unknown"
		killNativeTerminal(j.command)
		select {
		case err = <-done:
		case <-time.After(time.Second):
		}
	}
	// A background native child may inherit the terminal. Never wait indefinitely
	// for its output, and never kill it just to collect console bytes.
	select {
	case <-drained:
	case <-time.After(100 * time.Millisecond):
	}
	_ = j.terminal.Close()
	if state == "unknown" {
		// Preserve the same shared panel fence as an unknown node activation.
		// Only independent recovery/readback may reopen panel mutations.
		m.Lease.Block()
	}
	m.mu.Lock()
	j.state = state
	if m.saveReceipt(j) != nil {
		j.state = "unknown"
		state = "unknown"
		m.Lease.Block()
	}
	if state != "unknown" {
		code := 0
		if err != nil {
			code = -1
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				code = exit.ExitCode()
			}
		}
		j.exit = &code
	}
	m.mu.Unlock()
}

func (m *Jobs) view(j *nativeJob, cursor int64) JobView {
	end := j.base + int64(len(j.output))
	truncated := cursor < j.base
	if cursor < j.base {
		cursor = j.base
	}
	if cursor > end {
		cursor = end
	}
	n := end - cursor
	if n > 32768 {
		n = 32768
	}
	start := cursor - j.base
	return JobView{ID: j.id, Action: j.action, State: j.state, Interactive: j.interactive, ExitCode: j.exit, Output: base64.StdEncoding.EncodeToString(j.output[start : start+n]), Cursor: cursor + n, Truncated: truncated}
}
func (m *Jobs) Read(owner, id string, cursor int64) (JobView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if owner == "" || m.job == nil || m.job.owner != "" && m.job.owner != owner || id != "" && m.job.id != id || cursor < 0 {
		return JobView{}, ErrJob
	}
	return m.view(m.job, cursor), nil
}
func (m *Jobs) Input(owner, id, data string) error {
	if len(data) == 0 || len(data) > 4096 {
		return ErrJob
	}
	m.mu.Lock()
	j := m.job
	if j == nil || j.owner != owner || j.id != id || j.state != "running" || !j.interactive {
		m.mu.Unlock()
		return ErrJob
	}
	terminal := j.terminal
	m.mu.Unlock()
	if terminal.SetWriteDeadline(time.Now().Add(time.Second)) != nil {
		return ErrJob
	}
	_, err := io.WriteString(terminal, data)
	if err != nil {
		return ErrJob
	}
	m.mu.Lock()
	j.lastActivity = time.Now()
	m.mu.Unlock()
	return nil
}
func (m *Jobs) Resize(owner, id string, cols, rows uint16) error {
	if cols < 20 || cols > 300 || rows < 5 || rows > 100 {
		return ErrJob
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.job
	if j == nil || j.owner != owner || j.id != id || j.state != "running" || !j.interactive {
		return ErrJob
	}
	return resizeNativeTerminal(j.terminal, cols, rows)
}
func (m *Jobs) Cancel(owner, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.job
	if j == nil || j.owner != owner || j.id != id || j.state != "running" {
		return ErrJob
	}
	j.cancel()
	return nil
}
