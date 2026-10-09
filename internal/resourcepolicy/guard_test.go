package resourcepolicy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func testGuard(busy []uint64) (*Guard, *atomic.Int32) {
	var calls atomic.Int32
	s := Sample{At: time.Unix(100, 0), TotalKiB: 256 * 1024, AvailableKiB: 100 * 1024}
	g := &Guard{Profile: ForPlatform("mipsle", 256*1024), Interval: time.Millisecond}
	g.Read = func() (Sample, error) {
		i := int(calls.Add(1)) - 1
		s.At = s.At.Add(time.Second)
		s.Total += 100
		load := uint64(0)
		if i < len(busy) {
			load = busy[i]
		}
		s.Idle += 100 - load
		return s, nil
	}
	return g, &calls
}

func TestAdmissionAndSustainedPressure(t *testing.T) {
	for _, tt := range []struct {
		name   string
		load   []uint64
		reject bool
	}{
		{"sustained", []uint64{0, 90, 90}, true},
		{"isolated burst", []uint64{0, 99, 0}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g, _ := testGuard(tt.load)
			_, stop, err := g.Start(context.Background())
			if tt.reject {
				if !errors.Is(err, ErrPressure) {
					t.Fatal(err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				stop()
			}
		})
	}
	g, calls := testGuard([]uint64{0, 0, 0, 99, 99, 99})
	ctx, stop, err := g.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("pressure did not cancel")
	}
	stop()
	if !errors.Is(context.Cause(ctx), ErrPressure) || calls.Load() != 6 {
		t.Fatal(context.Cause(ctx), calls.Load())
	}
	// Stop is idempotent and joins the sole sampler.
	stop()
}

func TestAdmissionMemorySwapTelemetryAndCancellation(t *testing.T) {
	for _, mode := range []string{"memory", "swap", "unknown", "cancel", "native"} {
		t.Run(mode, func(t *testing.T) {
			g, _ := testGuard(nil)
			read := g.Read
			g.Read = func() (Sample, error) {
				s, e := read()
				switch mode {
				case "memory":
					s.AvailableKiB = 1
				case "swap":
					s.SwapOut = uint64(s.At.Unix()) * 1024
				case "unknown":
					return s, ErrTelemetry
				}
				return s, e
			}
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := ErrPressure
			if mode == "unknown" {
				want = ErrTelemetry
			}
			if mode == "cancel" {
				cancel()
				want = context.Canceled
			}
			if mode == "native" {
				g.Conflict = func() (bool, error) { return true, nil }
				want = ErrExternalBenchmark
			}
			_, stop, err := g.Start(parent)
			if stop != nil {
				stop()
			}
			if !errors.Is(err, want) {
				t.Fatal(err, want)
			}
		})
	}
}

func TestBoundedProcReader(t *testing.T) {
	dir := t.TempDir()
	put := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	put("stat", "cpu  100 0 20 800 10 2 3 4 99 99\n")
	put("meminfo", "MemTotal: 254472 kB\nMemAvailable: 107780 kB\n")
	put("vmstat", "pswpout 100\n")
	s, err := ReadProc(dir)
	if err != nil || s.Total != 939 || s.Idle != 810 || s.AvailableKiB != 107780 {
		t.Fatal(s, err)
	}
	put("meminfo", "MemTotal: 254472 kB\nMemFree: 107780 kB\n")
	if _, err := ReadProc(dir); !errors.Is(err, ErrTelemetry) {
		t.Fatal("invented available memory", err)
	}
	put("stat", fmt.Sprintf("cpu %s", make([]byte, 65536)))
	if _, err := ReadProc(dir); !errors.Is(err, ErrTelemetry) {
		t.Fatal("unbounded stat", err)
	}
}

func TestProfilesDoNotScaleTrafficWithNodeInventory(t *testing.T) {
	for _, tt := range []struct {
		arch        string
		ram         uint64
		constrained bool
	}{{"mipsle", 500000, true}, {"arm64", 254472, true}, {"arm64", 500924, false}, {"arm64", 0, true}} {
		p := ForPlatform(tt.arch, tt.ram)
		if p.Constrained != tt.constrained {
			t.Fatal(p)
		}
		if p.Constrained {
			m, c := p.Manual(), p.Comparison(true)
			if p.Automatic || m.Bytes != 4*MiB || m.Seconds != 20 || c.Bytes != 24*MiB || c.Seconds != 90 || c.Candidates != 3 || c.Attempts != 4 {
				t.Fatal(p, m, c)
			}
		}
	}
}
