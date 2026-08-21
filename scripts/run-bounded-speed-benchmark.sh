#!/bin/sh
set -eu

# Migration guard for stale installs. C.1 runs the fixed, append-only,
# streaming benchmark inside xkeen-control and never through this script.
echo "ERROR: legacy XKeen benchmark disabled; use authenticated xkeen-control Run benchmark" >&2
exit 1
