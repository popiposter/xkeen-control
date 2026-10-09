//go:build !linux

package resourcepolicy

func readSwapDiskWrites(string) (uint64, string, error) {
	return 0, "", ErrTelemetry
}
