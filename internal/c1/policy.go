package c1

import (
	"time"
)

const (
	MiB                        int64 = 1 << 20
	KiB                              = 1 << 10
	MaxRegistryNodes                 = 256
	DefaultLatencyWindow             = 15 * time.Minute
	DefaultLatencyObservations       = 3
)

// Policy is the fixed runtime policy for the panel's measurement owner.
type Policy struct {
	Enabled             bool
	LatencyWindow       time.Duration
	LatencyObservations int
}

func DefaultPolicy() Policy {
	return Policy{Enabled: true, LatencyWindow: DefaultLatencyWindow, LatencyObservations: DefaultLatencyObservations}
}

func (p Policy) normalized() Policy {
	if p.LatencyWindow <= 0 {
		p.LatencyWindow = DefaultLatencyWindow
	}
	if p.LatencyObservations <= 0 {
		p.LatencyObservations = DefaultLatencyObservations
	}
	return p
}

type NodeState struct {
	ID      string
	Tag     string
	Enabled bool
}
