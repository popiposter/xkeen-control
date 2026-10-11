// Package resourcepolicy supplies read-only hardware bounds to existing work
// owners. It does not persist settings, schedule work or own router recovery.
package resourcepolicy

import "time"

const MiB int64 = 1 << 20

type Profile struct {
	Name        string `json:"name"`
	Constrained bool   `json:"constrained"`
	Automatic   bool   `json:"automatic"`
}

func ForPlatform(arch string, memoryKiB uint64) Profile {
	if arch == "mips" || arch == "mipsle" {
		return Profile{Name: "constrained", Constrained: true, Automatic: true}
	}
	if memoryKiB == 0 || memoryKiB <= 256*1024 {
		// A low-memory router uses the constrained limits with the same
		// automatic policy as MIPS (plan A8).
		return Profile{Name: "constrained", Constrained: true, Automatic: true}
	}
	return Profile{Name: "standard", Automatic: true}
}

type Limits struct {
	Download []int64 `json:"-"`
	Upload   []int64 `json:"-"`
	// EarlyStop ends a direction's ladder after a stage at least this long;
	// CandidateSeconds bounds one node's down+up work. Zero keeps the
	// adaptive defaults (1 s and 27 s).
	EarlyStop        time.Duration `json:"-"`
	CandidateSeconds int           `json:"-"`
	Bytes            int64         `json:"bytes"`
	Seconds          int           `json:"seconds"`
	Candidates       int           `json:"candidates"`
	Attempts         int           `json:"attempts"`
}

func (l Limits) Wall() time.Duration { return time.Duration(l.Seconds) * time.Second }

func (p Profile) Manual() Limits {
	if p.Constrained {
		return Limits{Download: []int64{MiB / 2, MiB * 5 / 2}, Upload: []int64{MiB / 4, MiB * 3 / 4}, Bytes: 4 * MiB, Seconds: 20, Candidates: 1, Attempts: 1}
	}
	return Limits{Download: []int64{MiB, 3 * MiB, 8 * MiB, 20 * MiB}, Upload: []int64{MiB, 3 * MiB, 4 * MiB, 8 * MiB}, Bytes: 48 * MiB, Seconds: 45, Candidates: 1, Attempts: 1}
}

func (p Profile) Comparison(broad bool) Limits {
	if p.Constrained {
		return Limits{Download: []int64{MiB, 3 * MiB}, Upload: []int64{MiB / 2, MiB * 3 / 2}, Bytes: 24 * MiB, Seconds: 90, Candidates: 3, Attempts: 4}
	}
	// Larger stages and a 3 s early stop measure fast nodes by sustained
	// throughput rather than connection setup (PC benchmark 2026-10-12: the
	// 8 MiB ladder read a 234 Mbps node as ~100). Per node at most 60 MiB
	// down + 28 MiB up within 45 s; six nodes per batch.
	l := Limits{Download: []int64{MiB, 3 * MiB, 8 * MiB, 16 * MiB, 32 * MiB}, Upload: []int64{MiB, 3 * MiB, 8 * MiB, 16 * MiB}, EarlyStop: 3 * time.Second, CandidateSeconds: 45, Bytes: 540 * MiB, Seconds: 300, Candidates: 6, Attempts: 12}
	if broad {
		l.Bytes = 1080 * MiB
		l.Seconds = 600
		l.Candidates = 12
		l.Attempts = 18
	}
	return l
}

// ReviewLimits bound one quality review (REQ-008). Candidates is the frozen
// set probed by RTT; the speed phase runs them in sequential batches, each
// bounded by Comparison(false). Bytes and Wall cover the speed phase only and
// derive from the ladder: Candidates × per-candidate worst case, and
// batches × batch wall + pauses.
type ReviewLimits struct {
	Candidates int           `json:"candidates"`
	BatchSize  int           `json:"batchSize"`
	Pause      time.Duration `json:"-"`
	Bytes      int64         `json:"bytes"`
	Wall       time.Duration `json:"-"`
	// Repeat is how many of the best-ranked nodes a second pass measures
	// again; their two results are combined. Zero on constrained profiles.
	Repeat int `json:"repeat"`
}

// Review is the default review: eighteen speed-test candidates.
func (p Profile) Review() ReviewLimits {
	return p.ReviewFor(18)
}

// ReviewFor bounds a review of n speed-test candidates (clamped to 2..18).
// Constrained: n × 6 MiB (down 1/3, up 0.5/1.5), batches of three × 90 s
// with one-minute pauses; standard: n × 24 MiB (down 1/3/4/8, up 1/3/4),
// batches of six × 180 s. Both keep two minutes of cleanup margin.
func (p Profile) ReviewFor(n int) ReviewLimits {
	n = min(max(n, 2), 18)
	if p.Constrained {
		batches := (n + 2) / 3
		return ReviewLimits{Candidates: n, BatchSize: 3, Pause: time.Minute, Bytes: int64(n) * 6 * MiB, Wall: time.Duration(batches)*90*time.Second + time.Duration(batches-1)*time.Minute + 2*time.Minute}
	}
	// Standard: 88 MiB worst case per node, 300 s per batch of six, then a
	// second pass over the best eight.
	repeat := min(8, n)
	batches := (n+5)/6 + (repeat+5)/6
	return ReviewLimits{Candidates: n, BatchSize: 6, Bytes: int64(n+repeat) * 88 * MiB, Wall: time.Duration(batches)*300*time.Second + 2*time.Minute, Repeat: repeat}
}

func (p Profile) MemoryFloorKiB() uint64 {
	if p.Constrained {
		return 32 * 1024
	}
	return 64 * 1024
}
