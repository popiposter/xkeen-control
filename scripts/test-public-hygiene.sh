#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
if git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
	tracked="$(git -C "$ROOT" ls-files)"
else
	tracked="$(find "$ROOT" -type f -not -path '*/.git/*' -not -path '*/node_modules/*')"
fi
if printf '%s\n' "$tracked" | grep -E '(^|/)(nodes\.json|04_outbounds\.json|secrets/|auth/password|.*\.pem$|.*\.key$)' >/dev/null; then
	echo "forbidden secret-bearing tracked path" >&2
	exit 1
fi

if git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
	if git -C "$ROOT" grep -n -I -E 'BEGIN (OPENSSH|RSA|EC|ED25519) PRIVATE KEY|vless://[^[:space:]]+@[^[:space:]]+' -- ':!internal/nodes/parser_test.go' ':!internal/nodes/subscription_test.go' ':!internal/httpapi/mutation_test.go' ':!scripts/test-public-hygiene.sh' >/dev/null 2>&1; then
		echo "unexpected private key or production VLESS literal" >&2
		exit 1
	fi
elif grep -R -n -I -E --exclude-dir=.git --exclude-dir=node_modules --exclude='parser_test.go' --exclude='subscription_test.go' --exclude='mutation_test.go' --exclude='test-public-hygiene.sh' 'BEGIN (OPENSSH|RSA|EC|ED25519) PRIVATE KEY|vless://[^[:space:]]+@[^[:space:]]+' "$ROOT" >/dev/null 2>&1; then
	echo "unexpected private key or production VLESS literal" >&2
	exit 1
fi
if git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
	git -C "$ROOT" diff --check
fi
