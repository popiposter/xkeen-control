// Package setup coordinates explicit initial installation, not normal native
// admission. Native CLI/cron are independent of its panel process lock.
package setup

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"

	"github.com/popiposter/xkeen-control/internal/buildinfo"
	"github.com/popiposter/xkeen-control/internal/configjson"
)

const (
	LockPath    = "/opt/var/lock/xkeen-control/initial-setup.lock"
	ReceiptPath = "/opt/etc/xkeen-control/state/initial-setup.json"
	maxReceipt  = 8192
)

var (
	ErrState       = errors.New("initial setup state unsafe or incomplete; use root-only setup inspect")
	ErrBusy        = errors.New("initial setup or panel is already running")
	ErrUnsupported = errors.New("guided setup capability is unavailable; no mutation performed")
	digestPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	idPattern      = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

type Receipt struct {
	Schema             int            `json:"schemaVersion"`
	Release            buildinfo.Info `json:"release"`
	Phase              string         `json:"phase"`
	PolicyID           string         `json:"policyId,omitempty"`
	PolicyMark         uint32         `json:"policyMark,omitempty"`
	ProfileID          string         `json:"profileId,omitempty"`
	Generation         string         `json:"generation,omitempty"`
	JobID              string         `json:"jobId,omitempty"`
	PrerequisitesSaved bool           `json:"prerequisitesSaved"`
	FirmwareSaved      bool           `json:"firmwareSaved"`
	RuntimeVerified    bool           `json:"runtimeVerified"`
	StartupVerified    bool           `json:"startupVerified"`
}

func (r Receipt) valid() bool {
	if r.Schema != 1 || r.Release.Validate() != nil || r.Release.Channel == "development" {
		return false
	}
	switch r.Phase {
	case "prepared", "policy", "native", "panel", "dns", "candidate", "activation", "home", "unknown", "aborted", "completed":
	default:
		return false
	}
	for _, id := range []string{r.PolicyID, r.ProfileID, r.JobID} {
		if id != "" && !idPattern.MatchString(id) {
			return false
		}
	}
	if r.Generation != "" && !digestPattern.MatchString(r.Generation) {
		return false
	}
	if r.Phase == "completed" {
		return r.PolicyID != "" && r.PolicyMark != 0 && r.ProfileID != "" && r.Generation != "" &&
			r.PrerequisitesSaved && r.FirmwareSaved && r.RuntimeVerified && r.StartupVerified
	}
	return true
}

func decodeReceipt(b []byte) (Receipt, error) {
	var r Receipt
	if len(b) == 0 || len(b) > maxReceipt {
		return r, ErrState
	}
	if _, err := configjson.DecodeObject(b); err != nil {
		return r, ErrState
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || !r.valid() {
		return Receipt{}, ErrState
	}
	if d.Decode(new(any)) != io.EOF {
		return Receipt{}, ErrState
	}
	return r, nil
}

// Inspect never deletes a receipt or makes an interrupted setup replayable.
func Inspect() (*Receipt, error) { return readReceipt(ReceiptPath) }

// Normal acquires the shared process lock before checking persisted setup state.
// The returned close function must be held for the complete daemon/CLI lifetime.
func Normal() (func(), error) {
	return processAdmission(false)
}

// Maintenance excludes every cooperating daemon/CLI for the complete offline
// operation. It neither adopts setup state nor inherits an installation lock.
func Maintenance() (func(), error) { return processAdmission(true) }

func processAdmission(exclusive bool) (func(), error) {
	l, err := acquireLock(LockPath, exclusive)
	if err != nil {
		return nil, err
	}
	r, err := readReceipt(ReceiptPath)
	if err != nil || r != nil && r.Phase != "completed" {
		l()
		return nil, ErrState
	}
	return l, nil
}

func Guard() error {
	close, err := Normal()
	if err == nil {
		close()
	}
	return err
}
