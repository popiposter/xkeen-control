#!/usr/bin/env bash
set -euo pipefail

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
LOADER=$ROOT/scripts/keenetic-env.sh
TEMPORARY=$(mktemp -d)
trap 'rm -rf -- "$TEMPORARY"' EXIT

write_env() {
	local path=$1 contents=$2
	printf '%s' "$contents" > "$path"
	chmod 600 "$path"
}

run_loader() {
	local path=$1 output=$2
	(
		unset KEENETIC_SSH_HOST KEENETIC_SSH_PORT KEENETIC_SSH_USER KEENETIC_SSH_TARGET KEENETIC_SSH_PASSWORD KEENETIC_SSH_IDENTITY_FILE
		export KEENETIC_ENV_FILE=$path
		# shellcheck source=/dev/null
		source "$LOADER"
		[[ "$KEENETIC_SSH_TARGET" == *@* ]]
	) >"$output" 2>&1
}

assert_rejected() {
	local name=$1 contents=$2 path output
	path=$TEMPORARY/reject-$name.env
	output=$TEMPORARY/reject-$name.out
	write_env "$path" "$contents"
	if run_loader "$path" "$output"; then
		printf 'rejection fixture was accepted: %s\n' "$name" >&2
		exit 1
	fi
	if grep -Fq 'synthetic-secret-value' "$output"; then
		printf 'rejection exposed the synthetic secret: %s\n' "$name" >&2
		exit 1
	fi
}

mkdir -p "$TEMPORARY/keys"
printf '%s\n' 'synthetic-private-key-placeholder' > "$TEMPORARY/keys/operator-key"
chmod 600 "$TEMPORARY/keys/operator-key"

key_only=$TEMPORARY/key-only.env
write_env "$key_only" $'KEENETIC_SSH_HOST=router.example\nKEENETIC_SSH_PORT=222\nKEENETIC_SSH_USER=root\nKEENETIC_SSH_IDENTITY_FILE=keys/operator-key\n'
key_output=$TEMPORARY/key-only.out
(
	export KEENETIC_SSH_PASSWORD=stale-password KEENETIC_ENV_FILE=$key_only
	# shellcheck source=/dev/null
	source "$LOADER"
	[[ "$KEENETIC_SSH_TARGET" == 'root@router.example' ]]
	[[ "$KEENETIC_SSH_IDENTITY_FILE" == "$TEMPORARY/keys/operator-key" ]]
	[[ -z "${KEENETIC_SSH_PASSWORD+x}" ]]
) >"$key_output" 2>&1
[[ ! -s "$key_output" ]]

password_only=$TEMPORARY/password-only.env
write_env "$password_only" $'KEENETIC_SSH_HOST=router.example\nKEENETIC_SSH_PORT=22\nKEENETIC_SSH_USER=operator\nKEENETIC_SSH_PASSWORD=synthetic-secret-value\n'
password_output=$TEMPORARY/password-only.out
(
	export KEENETIC_SSH_IDENTITY_FILE=stale-key KEENETIC_ENV_FILE=$password_only
	# shellcheck source=/dev/null
	source "$LOADER"
	[[ "$KEENETIC_SSH_PASSWORD" == 'synthetic-secret-value' ]]
	[[ -z "${KEENETIC_SSH_IDENTITY_FILE+x}" ]]
) >"$password_output" 2>&1
[[ ! -s "$password_output" ]]

both=$TEMPORARY/both.env
write_env "$both" $'KEENETIC_SSH_HOST=router.example\nKEENETIC_SSH_PORT=2200\nKEENETIC_SSH_USER=operator\nKEENETIC_SSH_PASSWORD="synthetic-secret-value"\nKEENETIC_SSH_IDENTITY_FILE="keys/operator-key"\n'
both_output=$TEMPORARY/both.out
run_loader "$both" "$both_output"
[[ ! -s "$both_output" ]]

base=$'KEENETIC_SSH_HOST=router.example\nKEENETIC_SSH_PORT=22\nKEENETIC_SSH_USER=root\nKEENETIC_SSH_PASSWORD=synthetic-secret-value\n'
assert_rejected unknown "${base}UNSUPPORTED=value"$'\n'
assert_rejected duplicate "${base}KEENETIC_SSH_HOST=other.example"$'\n'
assert_rejected missing-user $'KEENETIC_SSH_HOST=router.example\nKEENETIC_SSH_PORT=22\nKEENETIC_SSH_PASSWORD=synthetic-secret-value\n'
assert_rejected invalid-port "${base/KEENETIC_SSH_PORT=22/KEENETIC_SSH_PORT=70000}"
assert_rejected no-auth $'KEENETIC_SSH_HOST=router.example\nKEENETIC_SSH_PORT=22\nKEENETIC_SSH_USER=root\n'

oversize=$TEMPORARY/oversize.env
head -c 4097 /dev/zero | tr '\0' A > "$oversize"
chmod 600 "$oversize"
if run_loader "$oversize" "$TEMPORARY/oversize.out"; then
	printf '%s\n' 'oversize fixture was accepted' >&2
	exit 1
fi

ln -s "$password_only" "$TEMPORARY/linked.env"
if run_loader "$TEMPORARY/linked.env" "$TEMPORARY/linked.out"; then
	printf '%s\n' 'symlink fixture was accepted' >&2
	exit 1
fi

cp "$password_only" "$TEMPORARY/permissive.env"
chmod 644 "$TEMPORARY/permissive.env"
if run_loader "$TEMPORARY/permissive.env" "$TEMPORARY/permissive.out"; then
	printf '%s\n' 'group-readable fixture was accepted' >&2
	exit 1
fi

printf '%s\n' 'keenetic env Bash fixtures passed'
