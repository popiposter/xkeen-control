package release

import (
	"os"
	"strings"
	"testing"
)

// Deliberately narrow contract for the existing workflow, not a YAML interpreter.
func releaseBrowserBoundary(workflow string) bool {
	workflow = strings.ReplaceAll(workflow, "\r\n", "\n")
	_, jobs, ok := strings.Cut(workflow, "\njobs:\n  build:\n")
	if !ok {
		return false
	}
	build, publish, ok := strings.Cut(jobs, "\n  publish:\n")
	if !ok || !strings.HasPrefix(publish, "    needs: build\n") {
		return false
	}
	install := strings.Index(build, "\n          npm --prefix web ci --ignore-scripts\n")
	browser := strings.Index(build, "\n          (cd web && npx playwright install --with-deps chromium && npm run test:components-ui)\n")
	handoff := strings.Index(build, "\n      - name: Assemble unsigned deterministic release inputs\n")
	return install >= 0 && browser > install && handoff > browser &&
		!strings.Contains(build, "continue-on-error:") && !strings.Contains(build, "environment:") &&
		!strings.Contains(publish, "playwright") && !strings.Contains(publish, "test:components-ui")
}

func TestReleaseBrowserBuildBoundary(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	if !releaseBrowserBoundary(workflow) {
		t.Fatal("release build must run the pinned F2 Chromium suite after npm ci and before unsigned handoff; publish must depend on build")
	}
	for name, mutated := range map[string]string{
		"removed suite": strings.ReplaceAll(workflow, "npm run test:components-ui", "true"),
		"ignored failure": strings.ReplaceAll(workflow, "npm run test:components-ui)", "npm run test:components-ui) || true"),
		"detached publish": strings.ReplaceAll(workflow, "needs: build", "needs: []"),
	} {
		t.Run(name, func(t *testing.T) {
			if releaseBrowserBoundary(mutated) {
				t.Fatal("regression accepted unsafe workflow")
			}
		})
	}
}
