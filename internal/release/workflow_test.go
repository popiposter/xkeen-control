package release

import (
	"os"
	"strings"
	"testing"
)

// Deliberately narrow contract for the existing workflow, not a YAML interpreter.
func releaseQualificationBoundary(workflow, devCheck string) bool {
	workflow = strings.ReplaceAll(workflow, "\r\n", "\n")
	devCheck = strings.ReplaceAll(devCheck, "\r\n", "\n")
	_, jobs, ok := strings.Cut(workflow, "\njobs:\n  build:\n")
	if !ok {
		return false
	}
	build, publish, ok := strings.Cut(jobs, "\n  publish:\n")
	if !ok || !strings.HasPrefix(publish, "    needs: build\n") {
		return false
	}
	full := strings.Index(build, "\n          bash scripts/dev-check.sh --full\n")
	handoff := strings.Index(build, "\n      - name: Assemble unsigned deterministic release inputs\n")
	install := strings.Index(devCheck, "\tnpm --prefix web ci --ignore-scripts --prefer-offline\n")
	browser := strings.Index(devCheck, "\t\tnpm --prefix web run test:ui\n")
	return full >= 0 && handoff > full && install >= 0 && browser > install &&
		strings.Contains(build, "XKEEN_PLAYWRIGHT_INSTALL: \"1\"") &&
		!strings.Contains(build, "continue-on-error:") && !strings.Contains(build, "environment:") &&
		!strings.Contains(build[full:handoff], "|| true") &&
		!strings.Contains(devCheck[install:browser+len("\t\tnpm --prefix web run test:ui\n")], "|| true") &&
		!strings.Contains(publish, "playwright") && !strings.Contains(publish, "test:ui")
}

func TestReleaseQualificationBoundary(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	devCheckData, err := os.ReadFile("../../scripts/dev-check.sh")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	devCheck := string(devCheckData)
	if !releaseQualificationBoundary(workflow, devCheck) {
		t.Fatal("release build must run the shared full qualification with pinned Chromium before unsigned handoff; publish must depend on build")
	}
	for name, pair := range map[string][2]string{
		"removed full gate":     {strings.ReplaceAll(workflow, "bash scripts/dev-check.sh --full", "true"), devCheck},
		"ignored full failure":  {strings.ReplaceAll(workflow, "bash scripts/dev-check.sh --full", "bash scripts/dev-check.sh --full || true"), devCheck},
		"removed browser suite": {workflow, strings.ReplaceAll(devCheck, "npm --prefix web run test:ui", "true")},
		"detached publish":      {strings.ReplaceAll(workflow, "needs: build", "needs: []"), devCheck},
	} {
		t.Run(name, func(t *testing.T) {
			if releaseQualificationBoundary(pair[0], pair[1]) {
				t.Fatal("regression accepted unsafe workflow")
			}
		})
	}
}
