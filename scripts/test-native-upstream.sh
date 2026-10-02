#!/usr/bin/env bash
set -euo pipefail
# Explicit public-input fixture catalogue. No download, router or complete native
# top-level execution. These inputs are hash-pinned inside the fixture builders.
: "${XKEEN_ADMISSION_INIT:?pinned public init input required}"
: "${XKEEN_ADMISSION_DISPATCHER:?pinned public dispatcher input required}"
: "${XKEEN_ADMISSION_REGISTER:?pinned public registration input required}"
: "${XKEEN_ADMISSION_INSTALLER:?pinned public installer input required}"
[[ ${XKEEN_ADMISSION_UPSTREAM:-0} != 1 ]] || { echo 'upstream counterexamples require individual fixture invocation' >&2; exit 1; }
node --test \
    scripts/native-admission-patch.test.mjs \
    scripts/native-admission-registration.test.mjs \
    scripts/native-admission-update-patch.test.mjs \
    scripts/native-admission-native-errors.test.mjs \
    scripts/native-admission-monitor.test.mjs \
    scripts/native-admission-hook.test.mjs \
    scripts/native-admission-hook-entry.test.mjs \
    scripts/native-admission-connected.test.mjs \
    scripts/native-admission-background.test.mjs
