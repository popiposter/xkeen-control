# Operator-local Keenetic SSH environment

Router qualification is host-side and must be explicitly authorized by the
active issue and operator. Loading local connection values grants no permission
to observe or mutate a router.

Copy `.env.keenetic.example` to the ignored `.env.keenetic` and replace the
placeholders locally. Never commit, paste or upload the real file. On POSIX,
make it private before loading:

```sh
chmod 600 .env.keenetic
```

On Windows, keep the file in the operator-owned checkout and restrict its ACL
to the operator account. The loaders reject links, oversized input, unknown or
duplicate keys, incomplete identity and invalid ports. They never print values.

Load the environment into the current shell:

```powershell
. .\scripts\keenetic-env.ps1
```

```bash
source scripts/keenetic-env.sh
```

The closed schema is:

- `KEENETIC_SSH_HOST`, `KEENETIC_SSH_PORT` and `KEENETIC_SSH_USER` — required;
- `KEENETIC_SSH_IDENTITY_FILE` — optional local private-key path;
- `KEENETIC_SSH_PASSWORD` — optional password for an operator-owned tool that
  deliberately consumes it.

At least one authentication method is required. Key-based access is preferred.
The identity file is checked as a small regular non-link file but its contents
are never read. Missing optional variables are removed from the shell on a
successful reload so stale credentials cannot survive a configuration change.

For key-based interactive access, pass the resolved identity explicitly:

```powershell
ssh -i $env:KEENETIC_SSH_IDENTITY_FILE -p $env:KEENETIC_SSH_PORT $env:KEENETIC_SSH_TARGET
```

```bash
ssh -i "$KEENETIC_SSH_IDENTITY_FILE" -p "$KEENETIC_SSH_PORT" "$KEENETIC_SSH_TARGET"
```

OpenSSH does not automatically consume `KEENETIC_SSH_PASSWORD`; use its normal
interactive prompt. Do not put a password in command arguments, shell history,
an askpass script, terminal recordings, GitHub, CI or logs. Avoid dumping the
environment after loading. Panel credentials, subscription material and node
registry contents are outside this helper and remain separate secret material.
