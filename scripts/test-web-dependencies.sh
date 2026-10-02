#!/usr/bin/env bash
set -euo pipefail
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/scripts" "$work/web" "$work/bin"
cp "$ROOT/scripts/web-dependencies.sh" "$work/scripts/"
printf '{}\n' > "$work/web/package.json"
printf '{}\n' > "$work/web/package-lock.json"
cat > "$work/bin/npm" <<'NPM'
#!/bin/sh
case "$1" in
    --version) echo fixture ;;
    ls) test -f node_modules/intact ;;
    ci) mkdir -p node_modules; touch node_modules/intact; echo ci >> installs ;;
    *) exit 1 ;;
esac
NPM
chmod +x "$work/bin/npm"
run() { PATH="$work/bin:$PATH" bash "$work/scripts/web-dependencies.sh" "$1"; }
run --reuse
run --reuse
test "$(wc -l < "$work/web/installs")" -eq 1
printf '{"changed":true}\n' > "$work/web/package-lock.json"
run --reuse
test "$(wc -l < "$work/web/installs")" -eq 2
rm "$work/web/node_modules/intact"
run --reuse
test "$(wc -l < "$work/web/installs")" -eq 3
run --clean
test "$(wc -l < "$work/web/installs")" -eq 4
echo 'Web dependency reuse fixtures passed'
