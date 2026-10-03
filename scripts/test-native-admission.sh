#!/usr/bin/env bash
set -euo pipefail
# Self-contained admission fixtures only. Pinned upstream artifact tests remain
# explicit focused checks; adding a fixture here does not alter browser dispatch.
node --test \
    scripts/native-operation-gate-prepare.test.mjs \
    scripts/native-admission-entry.test.mjs \
    scripts/native-admission-verify.test.mjs \
    scripts/native-admission-hook-verify.test.mjs \
    scripts/native-event-notification.test.mjs \
    scripts/native-update-context.test.mjs \
    scripts/native-update-packages.test.mjs \
    scripts/native-event-convergence.test.mjs
