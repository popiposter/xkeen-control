package resourcepolicy

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var ErrPressure = errors.New("router resources are busy; try again after other work finishes")
var ErrTelemetry = errors.New("router resource telemetry unavailable")
var ErrExternalBenchmark = errors.New("native periodic speed test is configured; inspect and quiesce it before testing")

type Sample struct {
	At                                           time.Time
	Total, Idle, AvailableKiB, TotalKiB, SwapOut uint64
	SwapSource                                   string
	SwapUnitBytes                                uint64
}

type Guard struct {
	Conflict func() (bool, error)
	Profile  Profile
	Read     func() (Sample, error)
	Interval time.Duration
}

func NewGuard() *Guard {
	read := func() (Sample, error) { return ReadProc("/proc") }
	s, _ := read()
	return &Guard{Profile: ForPlatform(runtime.GOARCH, s.TotalKiB), Read: read, Interval: time.Second}
}

func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrTelemetry
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 64*1024+1))
	if err != nil || len(b) > 64*1024 {
		return nil, ErrTelemetry
	}
	return b, nil
}

func ReadProc(root string) (Sample, error) {
	s := Sample{At: time.Now()}
	b, err := readBounded(filepath.Join(root, "stat"))
	if err != nil {
		return s, err
	}
	line, _, _ := strings.Cut(string(b), "\n")
	fields := strings.Fields(line)
	if len(fields) < 9 || fields[0] != "cpu" {
		return s, ErrTelemetry
	}
	for i := 1; i <= 8; i++ {
		n, e := strconv.ParseUint(fields[i], 10, 64)
		if e != nil || n > ^uint64(0)-s.Total {
			return s, ErrTelemetry
		}
		s.Total += n
		if i == 4 || i == 5 {
			s.Idle += n
		}
	}
	b, err = readBounded(filepath.Join(root, "meminfo"))
	if err != nil {
		return s, err
	}
	foundTotal, foundAvailable := false, false
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if f[0] != "MemTotal:" && f[0] != "MemAvailable:" {
			continue
		}
		if len(f) != 3 || f[2] != "kB" {
			return s, ErrTelemetry
		}
		n, e := strconv.ParseUint(f[1], 10, 64)
		if e != nil {
			return s, ErrTelemetry
		}
		if f[0] == "MemTotal:" {
			s.TotalKiB = n
			foundTotal = true
		} else {
			s.AvailableKiB = n
			foundAvailable = true
		}
	}
	if !foundTotal || !foundAvailable || s.TotalKiB == 0 || s.AvailableKiB > s.TotalKiB {
		return s, ErrTelemetry
	}
	b, err = readBounded(filepath.Join(root, "vmstat"))
	if err != nil {
		return s, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "pswpout" {
			s.SwapOut, err = strconv.ParseUint(f[1], 10, 64)
			if err != nil {
				return s, ErrTelemetry
			}
			s.SwapSource = "vmstat"
			s.SwapUnitBytes = uint64(os.Getpagesize())
			return s, nil
		}
	}
	s.SwapOut, s.SwapSource, err = readSwapDiskWrites(root)
	if err != nil {
		return s, ErrTelemetry
	}
	s.SwapUnitBytes = 512
	return s, nil
}

func swapRate(previous, current Sample) (float64, error) {
	dt := current.At.Sub(previous.At).Seconds()
	if dt <= 0 || previous.SwapSource != current.SwapSource || current.SwapOut < previous.SwapOut {
		return 0, ErrTelemetry
	}
	unit := current.SwapUnitBytes
	if unit == 0 {
		unit = uint64(os.Getpagesize())
	}
	if previous.SwapUnitBytes != current.SwapUnitBytes || unit == 0 {
		return 0, ErrTelemetry
	}
	return float64(current.SwapOut-previous.SwapOut) * float64(unit) / dt, nil
}

func pressure(previous, current Sample, floor uint64, cpuLimit float64) (bool, error) {
	if current.Total <= previous.Total || current.Idle < previous.Idle {
		return false, ErrTelemetry
	}
	total, idle := current.Total-previous.Total, current.Idle-previous.Idle
	if idle > total {
		return false, ErrTelemetry
	}
	swapBytesPerSecond, err := swapRate(previous, current)
	if err != nil {
		return false, err
	}
	cpu := 100 * float64(total-idle) / float64(total)
	return cpu >= cpuLimit || current.AvailableKiB < floor || swapBytesPerSecond > float64(MiB), nil
}

// Start checks two independent deltas before admitting work. During work it
// cancels only after three consecutive pressure samples. stop joins the sampler
// before its owner releases the existing operation lease. Probe cleanup remains
// with ProbeRouter and uses its independent bounded context.
func (g *Guard) Start(parent context.Context) (context.Context, func(), error) {
	if g == nil {
		return parent, func() {}, nil
	}
	if err := g.CheckConflict(); err != nil {
		return nil, nil, err
	}
	if g.Read == nil {
		return nil, nil, ErrTelemetry
	}
	interval := g.Interval
	if interval <= 0 {
		interval = time.Second
	}
	previous, err := g.Read()
	if err != nil {
		return nil, nil, ErrTelemetry
	}
	bad := 0
	for i := 0; i < 2; i++ {
		timer := time.NewTimer(interval)
		select {
		case <-parent.Done():
			timer.Stop()
			return nil, nil, parent.Err()
		case <-timer.C:
		}
		current, e := g.Read()
		if e != nil {
			return nil, nil, ErrTelemetry
		}
		busy, e := pressure(previous, current, g.Profile.MemoryFloorKiB(), 85)
		if e != nil {
			return nil, nil, e
		}
		swapBytesPerSecond, e := swapRate(previous, current)
		if e != nil {
			return nil, nil, e
		}
		if current.AvailableKiB < g.Profile.MemoryFloorKiB() || swapBytesPerSecond > float64(MiB) {
			return nil, nil, ErrPressure
		}
		if busy {
			bad++
		} else {
			bad = 0
		}
		previous = current
	}
	if bad >= 2 || previous.AvailableKiB < g.Profile.MemoryFloorKiB() {
		return nil, nil, ErrPressure
	}
	ctx, cancel := context.WithCancelCause(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(interval)
		defer tick.Stop()
		bad := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				current, e := g.Read()
				if e != nil {
					cancel(ErrTelemetry)
					return
				}
				busy, e := pressure(previous, current, g.Profile.MemoryFloorKiB(), 90)
				if e != nil {
					cancel(e)
					return
				}
				if busy {
					bad++
				} else {
					bad = 0
				}
				previous = current
				if bad >= 3 {
					cancel(ErrPressure)
					return
				}
			}
		}
	}()
	return ctx, func() { cancel(context.Canceled); <-done }, nil
}

func (g *Guard) CheckConflict() error {
	if g == nil || g.Conflict == nil {
		return nil
	}
	conflict, err := g.Conflict()
	if err != nil {
		return ErrTelemetry
	}
	if conflict {
		return ErrExternalBenchmark
	}
	return nil
}
