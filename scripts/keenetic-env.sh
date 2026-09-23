#!/usr/bin/env bash

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	printf '%s\n' 'source scripts/keenetic-env.sh from the current shell' >&2
	exit 2
fi

keenetic_env_error() {
	printf '%s\n' "Keenetic SSH environment: $1" >&2
}

keenetic_env_trim() {
	local value=$1
	value="${value#${value%%[![:space:]]*}}"
	value="${value%${value##*[![:space:]]}}"
	printf '%s' "$value"
}

keenetic_env_file_size() {
	local path=$1 result
	if result=$(stat -c '%s' -- "$path" 2>/dev/null); then
		:
	elif result=$(stat -f '%z' -- "$path" 2>/dev/null); then
		:
	else
		return 1
	fi
	[[ "$result" =~ ^[0-9]+$ ]] || return 1
	printf '%s' "$result"
}

keenetic_env_valid_host() {
	local value=$1 remainder label LC_ALL=C
	((${#value} >= 1 && ${#value} <= 253)) || return 1
	remainder=$value
	while [[ "$remainder" == *.* ]]; do
		label=${remainder%%.*}
		[[ "$label" =~ ^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$ ]] || return 1
		remainder=${remainder#*.}
	done
	[[ "$remainder" =~ ^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$ ]]
}

keenetic_env_valid_user() {
	local value=$1 LC_ALL=C
	((${#value} >= 1 && ${#value} <= 32)) || return 1
	[[ "$value" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]{0,31}$ ]]
}

keenetic_env_main() {
	local repository env_file env_directory line trimmed key value
	local host_value='' port_value='' user_value='' password_value='' identity_value=''
	local host_seen=0 port_seen=0 user_seen=0 password_seen=0 identity_seen=0
	local line_count=0 size mode directory candidate candidate_directory

	repository=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P) || return 1
	if [[ -z "${KEENETIC_ENV_FILE:-}" || "$KEENETIC_ENV_FILE" != /* ]]; then
		keenetic_env_error 'KEENETIC_ENV_FILE must be an explicit absolute host path'
		return 1
	fi
	env_file=$KEENETIC_ENV_FILE
	if [[ ! -f "$env_file" || -L "$env_file" ]]; then
		keenetic_env_error 'input must be a small regular non-link file'
		return 1
	fi
	env_directory=$(cd -- "$(dirname -- "$env_file")" && pwd -P) || return 1
	env_file=$env_directory/$(basename -- "$env_file")
	case "$env_file" in
		"$repository"|"$repository"/*)
			keenetic_env_error 'input must live outside the repository checkout'
			return 1 ;;
	esac
	size=$(keenetic_env_file_size "$env_file") || return 1
	if ((size <= 0 || size > 4096)); then
		keenetic_env_error 'input must be a small regular non-link file'
		return 1
	fi
	case "$(uname -s 2>/dev/null || true)" in
		MINGW*|MSYS*|CYGWIN*) ;;
		*)
			if mode=$(stat -c '%a' -- "$env_file" 2>/dev/null); then
				if ((8#$mode & 8#077)); then
					keenetic_env_error 'input permissions must exclude group and other access'
					return 1
				fi
			fi
			;;
	esac

	while IFS= read -r line || [[ -n "$line" ]]; do
		((line_count += 1))
		if ((line_count > 32)); then
			keenetic_env_error 'input has too many lines'
			return 1
		fi
		trimmed=$(keenetic_env_trim "$line")
		[[ -z "$trimmed" || ${trimmed:0:1} == '#' ]] && continue
		if [[ ! "$trimmed" =~ ^([A-Za-z_][A-Za-z0-9_]*)=(.*)$ ]]; then
			keenetic_env_error 'input contains an invalid line'
			return 1
		fi
		key=${BASH_REMATCH[1]}
		value=$(keenetic_env_trim "${BASH_REMATCH[2]}")
		if [[ ${#value} -ge 2 && (( ${value:0:1} == '"' && ${value: -1} == '"' ) || ( ${value:0:1} == "'" && ${value: -1} == "'" )) ]]; then
			value=${value:1:${#value}-2}
		elif [[ ${value:0:1} == '"' || ${value: -1} == '"' || ${value:0:1} == "'" || ${value: -1} == "'" ]]; then
			keenetic_env_error "input has unmatched quoting for: $key"
			return 1
		fi
		case "$key" in
			KEENETIC_SSH_HOST)
				((host_seen == 0)) || { keenetic_env_error "input repeats a key: $key"; return 1; }
				host_seen=1; host_value=$value ;;
			KEENETIC_SSH_PORT)
				((port_seen == 0)) || { keenetic_env_error "input repeats a key: $key"; return 1; }
				port_seen=1; port_value=$value ;;
			KEENETIC_SSH_USER)
				((user_seen == 0)) || { keenetic_env_error "input repeats a key: $key"; return 1; }
				user_seen=1; user_value=$value ;;
			KEENETIC_SSH_PASSWORD)
				((password_seen == 0)) || { keenetic_env_error "input repeats a key: $key"; return 1; }
				password_seen=1; password_value=$value ;;
			KEENETIC_SSH_IDENTITY_FILE)
				((identity_seen == 0)) || { keenetic_env_error "input repeats a key: $key"; return 1; }
				identity_seen=1; identity_value=$value ;;
			*)
				keenetic_env_error "input contains an unsupported key: $key"
				return 1 ;;
		esac
	done < "$env_file"

	if ((host_seen == 0 || port_seen == 0 || user_seen == 0)); then
		keenetic_env_error 'input is missing required SSH identity'
		return 1
	fi
	if ! keenetic_env_valid_host "$host_value"; then
		keenetic_env_error 'host must use 1..253 ASCII characters in 1..63 character alphanumeric/hyphen labels'
		return 1
	fi
	if ! keenetic_env_valid_user "$user_value"; then
		keenetic_env_error 'user must contain 1..32 option-safe ASCII characters'
		return 1
	fi
	if [[ ! "$port_value" =~ ^[0-9]+$ ]] || ((10#$port_value < 1 || 10#$port_value > 65535)); then
		keenetic_env_error 'port is outside 1..65535'
		return 1
	fi

	if ((identity_seen)); then
		if [[ -z "$identity_value" ]]; then
			keenetic_env_error 'identity file is empty'
			return 1
		fi
		if [[ "$identity_value" == /* || "$identity_value" =~ ^[A-Za-z]:[/\\] ]]; then
			candidate=$identity_value
		else
			directory=$(cd -- "$(dirname -- "$env_file")" && pwd -P) || return 1
			candidate=$directory/$identity_value
		fi
		if [[ ! -f "$candidate" || -L "$candidate" ]]; then
			keenetic_env_error 'identity file must be a small regular non-link file'
			return 1
		fi
		candidate_directory=$(cd -- "$(dirname -- "$candidate")" && pwd -P) || return 1
		candidate=$candidate_directory/$(basename -- "$candidate")
		case "$candidate" in
			"$repository"|"$repository"/*)
				keenetic_env_error 'identity file must live outside the repository checkout'
				return 1 ;;
		esac
		size=$(keenetic_env_file_size "$candidate") || return 1
		if ((size <= 0 || size > 65536)); then
			keenetic_env_error 'identity file must be a small regular non-link file'
			return 1
		fi
		identity_value=$candidate
	fi
	if [[ -z "$password_value" && -z "$identity_value" ]]; then
		keenetic_env_error 'a password or identity file is required'
		return 1
	fi

	export KEENETIC_SSH_HOST=$host_value
	export KEENETIC_SSH_PORT=$port_value
	export KEENETIC_SSH_USER=$user_value
	export KEENETIC_SSH_TARGET=$user_value@$host_value
	if [[ -n "$password_value" ]]; then
		export KEENETIC_SSH_PASSWORD=$password_value
	else
		unset KEENETIC_SSH_PASSWORD
	fi
	if [[ -n "$identity_value" ]]; then
		export KEENETIC_SSH_IDENTITY_FILE=$identity_value
	else
		unset KEENETIC_SSH_IDENTITY_FILE
	fi
}

if keenetic_env_main; then
	unset -f keenetic_env_main keenetic_env_valid_user keenetic_env_valid_host keenetic_env_file_size keenetic_env_trim keenetic_env_error
	return 0
else
	unset -f keenetic_env_main keenetic_env_valid_user keenetic_env_valid_host keenetic_env_file_size keenetic_env_trim keenetic_env_error
	return 1
fi
