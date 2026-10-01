#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
test "$(id -u)" -eq 0
temporary=$(mktemp -d /tmp/xkeen-release-git-trust.XXXXXX)
trap 'rm -rf "$temporary"' EXIT
# Isolate the fixture from any inherited runner/global trust exceptions.
export GIT_CONFIG_NOSYSTEM=1
export GIT_CONFIG_GLOBAL="$temporary/step.gitconfig"
unset SUDO_UID
touch "$GIT_CONFIG_GLOBAL"
export GITHUB_WORKSPACE="$temporary/checkout"
other="$temporary/unrelated"
for dir in "$GITHUB_WORKSPACE" "$other"; do
  git init --quiet "$dir"
  git -C "$dir" -c user.name=Fixture -c user.email=fixture@example.invalid commit --quiet --allow-empty -m fixture
done
expected=$(git -C "$GITHUB_WORKSPACE" rev-parse HEAD)
chown -R 1001:1001 "$GITHUB_WORKSPACE" "$other"

# checkout's own safe.directory lives in a temporary config, not the next step's.
git config --file "$temporary/action.gitconfig" --add safe.directory "$GITHUB_WORKSPACE"
test "$(GIT_CONFIG_GLOBAL="$temporary/action.gitconfig" git -C "$GITHUB_WORKSPACE" rev-parse HEAD)" = "$expected"
if git -C "$GITHUB_WORKSPACE" rev-parse HEAD >"$temporary/before.out" 2>"$temporary/before.err"; then
  echo 'foreign checkout unexpectedly trusted before workflow setup' >&2
  exit 1
fi
grep -q 'detected dubious ownership' "$temporary/before.err"

# Execute the actual workflow step, so its ownership behavior is covered.
awk '
  /^      - name: Trust exact checkout in root build job$/ { step=1; next }
  step && /^      - / { exit }
  step && /^        run: \|$/ { run=1; next }
  step && run { sub(/^          /, ""); print }
' "$ROOT/.github/workflows/release.yml" >"$temporary/trust.sh"
test -s "$temporary/trust.sh"
bash -euo pipefail "$temporary/trust.sh"
test "$(git -C "$GITHUB_WORKSPACE" rev-parse HEAD)" = "$expected"
# A later process must retain trust, including release-build.sh's Git reads.
bash -euc 'test "$(git -C "$GITHUB_WORKSPACE" rev-parse HEAD)" = "$1"' -- "$expected"
test "$(git config --global --get-all safe.directory)" = "$GITHUB_WORKSPACE"
if git -C "$other" rev-parse HEAD >"$temporary/other.out" 2>"$temporary/other.err"; then
  echo 'workflow setup trusted an unrelated foreign repository' >&2
  exit 1
fi
grep -q 'detected dubious ownership' "$temporary/other.err"
if test "$(git -C "$GITHUB_WORKSPACE" rev-parse HEAD)" = 0000000000000000000000000000000000000000; then
  echo 'wrong expected source was accepted' >&2
  exit 1
fi
echo 'release checkout trust fixture: PASS (foreign ownership, subsequent step, unrelated repository, wrong source)'
