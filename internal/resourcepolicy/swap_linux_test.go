//go:build linux

package resourcepolicy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSwapDiskWritesFallback(t *testing.T) {
	root := t.TempDir()
	put := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	put("swaps", "Filename Type Size Used Priority\n/dev/sda1 partition 1047548 105648 -2\n")
	row := "8 1 sda1 1 0 40 0 1 0 1502344 0 0 0 0"
	put("diskstats", row+"\n8 2 sda2 1 0 40 0 1 0 99999999 0 0 0 0\n")
	identify := func(string) (deviceID, error) { return deviceID{8, 1}, nil }
	count, source, err := readSwapDiskWritesWith(root, identify)
	if err != nil || count != 1502344 || source != "diskstats:8:1" {
		t.Fatal(count, source, err)
	}
	for _, tc := range []struct{ name, swaps, stats string }{
		{"multiple swap partitions", "Filename Type Size Used Priority\n/dev/sda1 partition 100 1 -2\n/dev/sdb1 partition 100 1 -3\n", row},
		{"swap file", "Filename Type Size Used Priority\n/swapfile file 100 1 -2\n", row},
		{"duplicate disk row", "Filename Type Size Used Priority\n/dev/sda1 partition 100 1 -2\n", row + "\n" + row},
		{"malformed disk row", "Filename Type Size Used Priority\n/dev/sda1 partition 100 1 -2\n", "8 1 sda1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			put("swaps", tc.swaps)
			put("diskstats", tc.stats)
			if _, _, err := readSwapDiskWritesWith(root, identify); !errors.Is(err, ErrTelemetry) {
				t.Fatal(err)
			}
		})
	}
	put("swaps", "Filename Type Size Used Priority\n/dev/sda1 partition 1047548 105648 -2\n")
	put("diskstats", row)
	if _, _, err := readSwapDiskWritesWith(root, func(string) (deviceID, error) { return deviceID{}, ErrTelemetry }); !errors.Is(err, ErrTelemetry) {
		t.Fatal(err)
	}
	put("stat", "cpu  100 0 20 800 10 2 3 4\n")
	put("meminfo", "MemTotal: 254472 kB\nMemAvailable: 136008 kB\n")
	put("vmstat", "pgpgout 123\n")
	// ReadProc normally resolves the real block device; use a parser seam above
	// for the synthetic fixture and verify the vmstat preference here.
	put("vmstat", "pswpout 50\n")
	s, err := ReadProc(root)
	if err != nil || s.SwapSource != "vmstat" || s.SwapOut != 50 {
		t.Fatal(s, err)
	}
}

func TestSwapRateSourceAndThreshold(t *testing.T) {
	a := Sample{At: time.Unix(0, 0), SwapOut: 100, SwapSource: "diskstats:8:1", SwapUnitBytes: 512}
	b := a
	b.At = a.At.Add(time.Second)
	b.SwapOut += 2048
	rate, err := swapRate(a, b)
	if err != nil || rate != float64(MiB) {
		t.Fatal(rate, err)
	}
	b.SwapOut++
	rate, err = swapRate(a, b)
	if err != nil || rate <= float64(MiB) {
		t.Fatal(rate, err)
	}
	for _, altered := range []Sample{
		func() Sample { x := b; x.SwapSource = "diskstats:8:2"; return x }(),
		func() Sample { x := b; x.SwapOut = 99; return x }(),
		func() Sample { x := b; x.SwapUnitBytes = 4096; return x }(),
	} {
		if _, err := swapRate(a, altered); !errors.Is(err, ErrTelemetry) {
			t.Fatal(err)
		}
	}
}
