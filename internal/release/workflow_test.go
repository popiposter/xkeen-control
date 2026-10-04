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
	checkout := strings.Index(build, "\n      - uses: actions/checkout@v7\n")
	root := strings.Index(build, "\n          test \"$(id -u)\" -eq 0\n")
	trust := strings.Index(build, "\n          git config --global --add safe.directory \"$GITHUB_WORKSPACE\"\n")
	identity := strings.Index(build, "$(git rev-parse HEAD)")
	tools := strings.Index(build, "\n          apt-get install --yes --no-install-recommends build-essential git jq\n")
	handoff := strings.Index(build, "\n      - name: Assemble unsigned deterministic release inputs\n")
	install := strings.Index(devCheck, "\t\tbash scripts/web-dependencies.sh --clean\n")
	browser := strings.Index(devCheck, "\t\tnpm --prefix web run test:ui\n")
	return checkout >= 0 && root > checkout && trust > root && identity > trust && tools > identity && full > tools && handoff > full && install >= 0 && browser > install &&
		strings.Contains(build, "\n    container:\n      image: node:24-bookworm\n      options: --user 0\n") &&
		strings.Contains(build, "\n    defaults:\n      run:\n        shell: bash\n") &&
		strings.Contains(build, "XKEEN_PLAYWRIGHT_INSTALL: \"1\"") &&
		!strings.Contains(build, "continue-on-error:") && !strings.Contains(build, "environment:") &&
		!strings.Contains(build[root:handoff], "|| true") &&
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
	workflow := strings.ReplaceAll(string(data), "\r\n", "\n")
	devCheck := strings.ReplaceAll(string(devCheckData), "\r\n", "\n")
	if !releaseQualificationBoundary(workflow, devCheck) {
		t.Fatal("release build must admit UID 0, trust only its exact checkout before reading Git identity, and run full qualification before unsigned handoff; publish must depend on build")
	}
	trustLine := "          git config --global --add safe.directory \"$GITHUB_WORKSPACE\"\n"
	lateTrust := strings.ReplaceAll(workflow, trustLine, "")
	lateTrust = strings.Replace(lateTrust, "      - name: Prepare root-owned qualification fixtures\n", trustLine+"      - name: Prepare root-owned qualification fixtures\n", 1)
	for name, pair := range map[string][2]string{
		"removed container":           {strings.ReplaceAll(workflow, "    container:\n      image: node:24-bookworm\n      options: --user 0\n", ""), devCheck},
		"nonroot container":           {strings.ReplaceAll(workflow, "options: --user 0", "options: --user 1001"), devCheck},
		"removed Bash default":        {strings.ReplaceAll(workflow, "shell: bash", "shell: sh"), devCheck},
		"removed UID admission":       {strings.ReplaceAll(workflow, "test \"$(id -u)\" -eq 0", "true"), devCheck},
		"ignored UID failure":         {strings.ReplaceAll(workflow, "test \"$(id -u)\" -eq 0", "test \"$(id -u)\" -eq 0 || true"), devCheck},
		"removed checkout trust":      {strings.ReplaceAll(workflow, "git config --global --add safe.directory \"$GITHUB_WORKSPACE\"", "true"), devCheck},
		"wildcard checkout trust":     {strings.ReplaceAll(workflow, "safe.directory \"$GITHUB_WORKSPACE\"", "safe.directory '*'"), devCheck},
		"local checkout trust":        {strings.ReplaceAll(workflow, "git config --global --add safe.directory", "git config --local --add safe.directory"), devCheck},
		"late checkout trust":         {lateTrust, devCheck},
		"ignored checkout trust":      {strings.ReplaceAll(workflow, "git config --global --add safe.directory \"$GITHUB_WORKSPACE\"", "git config --global --add safe.directory \"$GITHUB_WORKSPACE\" || true"), devCheck},
		"missing fixture tools":       {strings.ReplaceAll(workflow, "build-essential git jq", "git"), devCheck},
		"removed full gate":           {strings.ReplaceAll(workflow, "bash scripts/dev-check.sh --full", "true"), devCheck},
		"ignored full failure":        {strings.ReplaceAll(workflow, "bash scripts/dev-check.sh --full", "bash scripts/dev-check.sh --full || true"), devCheck},
		"removed browser suite":       {workflow, strings.ReplaceAll(devCheck, "npm --prefix web run test:ui", "true")},
		"reused release dependencies": {workflow, strings.ReplaceAll(devCheck, "web-dependencies.sh --clean", "web-dependencies.sh --reuse")},
		"detached publish":            {strings.ReplaceAll(workflow, "needs: build", "needs: []"), devCheck},
	} {
		t.Run(name, func(t *testing.T) {
			if releaseQualificationBoundary(pair[0], pair[1]) {
				t.Fatal("regression accepted unsafe workflow")
			}
		})
	}
}
