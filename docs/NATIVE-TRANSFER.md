# Native configuration transfer

Issue #121 uses native XKeen data rather than the historical `appliance.json`
backup model. XKeen programs, init scripts, package state, firewall state,
authentication, panel listener settings and process identities are excluded.

## Current implementation boundary

The source implementation exports one encrypted archive through the existing
authenticated, CSRF-protected, current-password-confirmed secret download.
The native service is connected in the main program. It is not yet installed
on the router. The native import backend now has read-only destination validation,
socket-interface mapping and session-bound Preview/Stage. Its HTTP integration
and the replacement transfer screen remain incomplete; the historical restore
service remains unwired.
Opening a native archive is read-only and cannot apply it through the historical
appliance importer.

The encrypted envelope retains the existing fixed Argon2id and
XChaCha20-Poly1305 parameters and the 6 MiB plaintext / 9 MiB envelope limits.
The same one-operation RAM/KDF budget is reserved before snapshot collection,
encoding and encryption. Plaintext downloads are unavailable for native data.

The decrypted `xkeen-control-native-data`, version 1 payload contains:

- A creation timestamp.
- Lossless JSON/JSONC bytes for the fixed native `01_log.json` through
  `08_api.json` IDs that are actually present, including `04_outbounds.json`.
- The canonical private managed-node/subscription registry. Positive absence
  becomes an empty registry without creating a file on the source router.

Unknown file IDs, non-object configs, invalid registries and a mismatch between
managed runtime outbounds and registry profiles are rejected. Native unmanaged
outbounds and their order remain intact. Pending saved configurations prevent
export, so the archive cannot silently combine applied and staged settings.
The panel lease covers config and registry snapshots; final reads detect changed
config bytes and registry appearance/content. This does not exclude external
native CLI or cron writers.

The import backend retains one private preview in RAM for five minutes. Missing
destination interfaces produce a mapping step without a Stage token. Mapping
splices only the selected interface string; surrounding JSONC bytes survive.
A ready preview has passed complete destination Xray validation without writes.
Stage consumes its session-bound token, rechecks the destination baseline and
validates again before saving. It performs no Restart.

Registry, generated outbounds and native config changes use the existing editor
pending generation, discard and optional previous-generation restoration. A
fixed private registry sidecar participates in the baseline/history but is never
passed to Xray or exposed as a raw editor document. Discard restores original
file absence as well as original bytes. The native mode, client policy and
schedules remain destination-native and require an explicit operator check.

## Operator workflow (HTTP/UI integration pending)

1. Install unmodified XKeen on the destination using its native installer.
2. Open the encrypted archive privately and preview config IDs, node/subscription
   counts, local interface/policy references and required geodata files.
3. Resolve destination references explicitly. Use native commands for native
   schedules; never copy sourced scripts or cron state blindly.
4. Validate the complete destination Xray candidate before saving data. Registry
   and generated outbounds must remain one coherent state.
5. Stage the validated set in the existing editor pending generation. Apply all
   changes by one explicit native Restart; retain pre-apply discard and optional
   previous-generation restoration. Do not create a second recovery owner.
6. Inspect the actual process/config health independently. Unknown Restart
   outcomes are inspected rather than replayed.

Second-router hardware acceptance is separate from encrypted-format fixtures.
