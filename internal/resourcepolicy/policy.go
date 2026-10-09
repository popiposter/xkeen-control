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
		return Profile{Name: "constrained", Constrained: true}
	}
	return Profile{Name: "standard", Automatic: true}
}

type Limits struct {
	Download   []int64 `json:"-"`
	Upload     []int64 `json:"-"`
	Bytes      int64   `json:"bytes"`
	Seconds    int     `json:"seconds"`
	Candidates int     `json:"candidates"`
	Attempts   int     `json:"attempts"`
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
	l := Limits{Download: []int64{MiB, 3 * MiB, 4 * MiB, 8 * MiB}, Upload: []int64{MiB, 3 * MiB, 4 * MiB}, Bytes: 144 * MiB, Seconds: 180, Candidates: 6, Attempts: 12}
	if broad {
		l.Bytes = 288 * MiB
		l.Seconds = 360
		l.Candidates = 12
		l.Attempts = 18
	}
	return l
}

func (p Profile) MemoryFloorKiB() uint64 {
	if p.Constrained {
		return 32 * 1024
	}
	return 64 * 1024
}
