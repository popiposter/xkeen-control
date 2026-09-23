# Operator-local Keenetic SSH environment

Router qualification is host-side and must be explicitly authorized by the
active issue and operator. Loading local connection values grants no permission
to observe or mutate a router.

Copy `.env.keenetic.example` to an operator-owned location **outside every
repository checkout** and replace the placeholders there. Never put the real
file or private identity below an `xkeen-control` checkout: Git ignore rules do
not keep files out of Docker bind mounts or build contexts. The ignored
`.env.keenetic` and `.env.keenetic.local` names are defense-in-depth only.

For example, on PowerShell:

```powershell
$keeneticDirectory = Join-Path ([Environment]::GetFolderPath('UserProfile')) '.config\xkeen-control'
New-Item -ItemType Directory -Path $keeneticDirectory -Force | Out-Null
$keeneticEnvironment = Join-Path $keeneticDirectory 'keenetic.env'
Copy-Item .\.env.keenetic.example $keeneticEnvironment
# Edit $keeneticEnvironment without printing it.
. .\scripts\keenetic-env.ps1 -EnvFile $keeneticEnvironment
```

On POSIX, make the external file private before loading it:

```sh
install -d -m 700 "$HOME/.config/xkeen-control"
install -m 600 .env.keenetic.example "$HOME/.config/xkeen-control/keenetic.env"
# Edit the external file without printing it.
export KEENETIC_ENV_FILE="$HOME/.config/xkeen-control/keenetic.env"
source scripts/keenetic-env.sh
```

On Windows, restrict the external file's ACL to the operator account. The
PowerShell loader requires `-EnvFile`; the Bash loader requires the absolute
host-only `KEENETIC_ENV_FILE` selector. Both reject the current checkout,
links, oversized input, unknown or duplicate keys, incomplete identity and
invalid ports. They never print values.

The closed schema is:

- `KEENETIC_SSH_HOST`, `KEENETIC_SSH_PORT` and `KEENETIC_SSH_USER` — required;
- `KEENETIC_SSH_IDENTITY_FILE` — optional local private-key path;
- `KEENETIC_SSH_PASSWORD` — optional password for an operator-owned tool that
  deliberately consumes it.

The host is 1..253 ASCII characters split into 1..63-character DNS/IPv4-style
labels; each label starts and ends alphanumeric and may contain internal
hyphens. The user is 1..32 ASCII characters, starts alphanumeric, and otherwise
contains only letters, digits, `_`, `.`, or `-`. This excludes whitespace,
controls, embedded `@`, leading-option targets and ambiguous host forms.

At least one authentication method is required. Key-based access is preferred.
The identity file is checked as a small regular non-link file but its contents
are never read. Missing optional variables are removed from the shell on a
successful reload so stale credentials cannot survive a configuration change.

For key-based interactive access, pass the resolved identity explicitly:

```powershell
ssh -i $env:KEENETIC_SSH_IDENTITY_FILE -p $env:KEENETIC_SSH_PORT -- $env:KEENETIC_SSH_TARGET
```

```bash
ssh -i "$KEENETIC_SSH_IDENTITY_FILE" -p "$KEENETIC_SSH_PORT" -- "$KEENETIC_SSH_TARGET"
```

OpenSSH does not automatically consume `KEENETIC_SSH_PASSWORD`; use its normal
interactive prompt. Do not put a password in command arguments, shell history,
an askpass script, terminal recordings, GitHub, CI or logs. Avoid dumping the
environment after loading. Panel credentials, subscription material and node
registry contents are outside this helper and remain separate secret material.
