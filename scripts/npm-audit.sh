#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
audit_log="$(mktemp)"
trap 'rm -f "$audit_log"' EXIT

for attempt in 1 2 3; do
	: >"$audit_log"
	if npm --prefix "$ROOT/web" audit --audit-level=high \
		--fetch-retries=1 \
		--fetch-retry-mintimeout=1000 \
		--fetch-retry-maxtimeout=5000 \
		--fetch-timeout=30000 2>&1 | tee "$audit_log"; then
		exit 0
	else
		status=$?
	fi
	if ! grep -Eqi 'network timeout|service unavailable|econnreset|etimedout|eai_again|audit endpoint returned an error|fetch failed' "$audit_log"; then
		exit "$status"
	fi
	if [ "$attempt" -eq 3 ]; then
		echo "npm audit advisory endpoint remained unavailable after 3 bounded attempts" >&2
		exit "$status"
	fi
	echo "npm audit advisory endpoint unavailable; retrying attempt $((attempt + 1))/3" >&2
	sleep $((attempt * 2))
done
