package xkeen

import "context"

// InspectQualitySettlement is a read-only lifecycle check for an explicit
// quality recovery. It never resolves an unknown job or claims it succeeded.
// ConfigureRecovery supplies the installed, bounded process inspection.
func (m *Jobs) InspectQualitySettlement(ctx context.Context) error {
	if m == nil {
		return ErrJob
	}
	m.mu.Lock()
	state := ""
	if m.job != nil {
		state = m.job.state
	}
	inspect := m.inspectRecovery
	m.mu.Unlock()
	if state == "running" || state == "unknown" || inspect == nil || ctx.Err() != nil {
		return ErrJob
	}
	if err := inspect(ctx); err != nil {
		return ErrJob
	}
	return nil
}
