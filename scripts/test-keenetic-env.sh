#!/usr/bin/env bash
set -euo pipefail

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
LOADER=$ROOT/scripts/keenetic-env.sh
TEMPORARY=$(mktemp -d)
trap 'rm -rf -- "$TEMPORARY"' EXIT

grep -Fxq '.env.keenetic' "$ROOT/.gitignore"
grep -Fxq '.env.keenetic.local' "$ROOT/.gitignore"
grep -Fxq '.env.keenetic' "$ROOT/.dockerignore"
grep -Fxq '.env.keenetic.local' "$ROOT/.dockerignore"

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

assert_accepted_target() {
	local name=$1 host_value=$2 user_value=$3 path output
	path=$TEMPORARY/accept-$name.env
	output=$TEMPORARY/accept-$name.out
	write_env "$path" "KEENETIC_SSH_HOST=$host_value
KEENETIC_SSH_PORT=22
KEENETIC_SSH_USER=$user_value
KEENETIC_SSH_PASSWORD=synthetic-secret-value
"
	(
		export KEENETIC_ENV_FILE=$path
		# shellcheck source=/dev/null
		source "$LOADER"
		[[ "$KEENETIC_SSH_TARGET" == "$user_value@$host_value" ]]
	) >"$output" 2>&1
	[[ ! -s "$output" ]]
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

maximum_host=$(printf '%063d.%063d.%063d.%061d' 0 0 0 0 | tr '0' a)
assert_accepted_target punctuation 'router-1.lab.example' 'operator_1.test-user'
assert_accepted_target bounds "$maximum_host" "$(printf '%032d' 0 | tr '0' u)"

base=$'KEENETIC_SSH_HOST=router.example\nKEENETIC_SSH_PORT=22\nKEENETIC_SSH_USER=root\nKEENETIC_SSH_PASSWORD=synthetic-secret-value\n'
assert_rejected unknown "${base}UNSUPPORTED=value"$'\n'
assert_rejected duplicate "${base}KEENETIC_SSH_HOST=other.example"$'\n'
assert_rejected missing-user $'KEENETIC_SSH_HOST=router.example\nKEENETIC_SSH_PORT=22\nKEENETIC_SSH_PASSWORD=synthetic-secret-value\n'
assert_rejected invalid-port "${base/KEENETIC_SSH_PORT=22/KEENETIC_SSH_PORT=70000}"
assert_rejected no-auth $'KEENETIC_SSH_HOST=router.example\nKEENETIC_SSH_PORT=22\nKEENETIC_SSH_USER=root\n'
assert_rejected host-leading-dash $'KEENETIC_SSH_HOST=-router.example\nKEENETIC_SSH_PORT=22\nKEENETIC_SSH_USER=root\nKEENETIC_SSH_PASSWORD=synthetic-secret-value\n'
assert_rejected host-trailing-dash $'KEENETIC_SSH_HOST=router-.example\nKEENETIC_SSH_PORT=22\nKEENETIC_SSH_USER=root\nKEENETIC_SSH_PASSWORD=synthetic-secret-value\n'
assert_rejected host-empty-label $'KEENETIC_SSH_HOST=router..example\nKEENETIC_SSH_PORT=22\nKEENETIC_SSH_USER=root\nKEENETIC_SSH_PASSWORD=synthetic-secret-value\n'
assert_rejected host-underscore $'KEENETIC_SSH_HOST=router_name.example\nKEENETIC_SSH_PORT=22\nKEENETIC_SSH_USER=root\nKEENETIC_SSH_PASSWORD=synthetic-secret-value\n'
assert_rejected host-at $'KEENETIC_SSH_HOST=router@example\nKEENETIC_SSH_PORT=22\nKEENETIC_SSH_USER=root\nKEENETIC_SSH_PASSWORD=synthetic-secret-value\n'
assert_rejected host-label-long "KEENETIC_SSH_HOST=$(printf '%064d' 0 | tr '0' a).example
KEENETIC_SSH_PORT=22
KEENETIC_SSH_USER=root
KEENETIC_SSH_PASSWORD=synthetic-secret-value
"
assert_rejected host-too-long "KEENETIC_SSH_HOST=${maximum_host}e
KEENETIC_SSH_PORT=22
KEENETIC_SSH_USER=root
KEENETIC_SSH_PASSWORD=synthetic-secret-value
"
assert_rejected user-leading-dash $'KEENETIC_SSH_HOST=router.example\nKEENETIC_SSH_PORT=22\nKEENETIC_SSH_USER=-root\nKEENETIC_SSH_PASSWORD=synthetic-secret-value\n'
assert_rejected user-at $'KEENETIC_SSH_HOST=router.example\nKEENETIC_SSH_PORT=22\nKEENETIC_SSH_USER=root@router\nKEENETIC_SSH_PASSWORD=synthetic-secret-value\n'
assert_rejected user-space $'KEENETIC_SSH_HOST=router.example\nKEENETIC_SSH_PORT=22\nKEENETIC_SSH_USER=root user\nKEENETIC_SSH_PASSWORD=synthetic-secret-value\n'
assert_rejected user-too-long "KEENETIC_SSH_HOST=router.example
KEENETIC_SSH_PORT=22
KEENETIC_SSH_USER=$(printf '%033d' 0 | tr '0' u)
KEENETIC_SSH_PASSWORD=synthetic-secret-value
"

if run_loader "$ROOT/.env.keenetic.example" "$TEMPORARY/checkout-local.out"; then
	printf '%s\n' 'checkout-local fixture was accepted' >&2
	exit 1
fi

# A public placeholder stands in for an identity, proving that its checkout
# location is rejected without reading it or creating a private key in Git.
assert_rejected checkout-identity-absolute "KEENETIC_SSH_HOST=router.example
KEENETIC_SSH_PORT=22
KEENETIC_SSH_USER=root
KEENETIC_SSH_IDENTITY_FILE=$ROOT/.env.keenetic.example
"
assert_rejected checkout-identity-dotdot "KEENETIC_SSH_HOST=router.example
KEENETIC_SSH_PORT=22
KEENETIC_SSH_USER=root
KEENETIC_SSH_IDENTITY_FILE=$ROOT/scripts/../.env.keenetic.example
"
ln -s "$ROOT" "$TEMPORARY/checkout-alias"
assert_rejected checkout-identity-parent-alias "KEENETIC_SSH_HOST=router.example
KEENETIC_SSH_PORT=22
KEENETIC_SSH_USER=root
KEENETIC_SSH_IDENTITY_FILE=$TEMPORARY/checkout-alias/.env.keenetic.example
"

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

# The supported Docker lane runs as root, so drop to nobody and prove that an
# unreadable identity is accepted from metadata alone. Any byte read fails.
if [[ "$(uname -s)" == Linux && "$(id -u)" == 0 ]]; then
	command -v su >/dev/null || { printf '%s\n' 'su is required for the metadata-only identity regression' >&2; exit 1; }
	metadata_directory=$TEMPORARY/metadata-only
	mkdir "$metadata_directory"
	metadata_identity=$metadata_directory/operator-key
	metadata_env=$metadata_directory/keenetic.env
	printf '%s\n' 'must-not-be-readable' > "$metadata_identity"
	write_env "$metadata_env" $'KEENETIC_SSH_HOST=router.example\nKEENETIC_SSH_PORT=22\nKEENETIC_SSH_USER=root\nKEENETIC_SSH_IDENTITY_FILE=operator-key\n'
	chmod 711 "$TEMPORARY"
	chown -R nobody:nogroup "$metadata_directory"
	chmod 700 "$metadata_directory"
	chmod 000 "$metadata_identity"
	export KEENETIC_ENV_FILE=$metadata_env KEENETIC_LOADER_FOR_TEST=$LOADER KEENETIC_IDENTITY_FOR_TEST=$metadata_identity
	if ! su --preserve-environment --shell /bin/bash nobody -c 'source "$KEENETIC_LOADER_FOR_TEST" && [[ "$KEENETIC_SSH_IDENTITY_FILE" == "$KEENETIC_IDENTITY_FOR_TEST" ]]' >"$TEMPORARY/metadata-only.out" 2>&1; then
		printf '%s\n' 'identity validation was not metadata-only' >&2
		exit 1
	fi
	unset KEENETIC_ENV_FILE KEENETIC_LOADER_FOR_TEST KEENETIC_IDENTITY_FOR_TEST
	[[ ! -s "$TEMPORARY/metadata-only.out" ]]
fi

printf '%s\n' 'keenetic env Bash fixtures passed'
