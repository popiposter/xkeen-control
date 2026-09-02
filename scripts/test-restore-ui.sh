#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

ui='web/src/main.jsx'

grep -Fq 'const effectiveMode = preview?.mode || mode' "$ui"
grep -Fq "const destructive = effectiveMode !== 'settings-only'" "$ui"
grep -Fq 'const canApply = Boolean(previewToken) && blockers.length === 0 && (!destructive || destructiveConfirmed)' "$ui"
grep -Fq 'disabled={restoreBusy} /> I understand this restore can replace or merge secret-bearing node registry state.' "$ui"
grep -Fq 'disabled={busy || !canApply}' "$ui"
grep -Fq "cause.status === 401 && cause.code === 'reauthentication failed'" "$ui"
grep -Fq 'const passphraseBytes = new TextEncoder().encode(secretPassphrase).length' "$ui"

echo 'Backup & Restore navigation/remount source regression passed'
