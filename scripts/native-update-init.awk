# Pure data projection for the fixed native update verifier. Never eval/source
# the init. The caller must bound and protect the input, require an LF-terminated
# file and compare code with the prepared registration template before effects.
# These are exactly variables_to_extract in the pinned native registration,
# plus its separately preserved start_auto/start_delay. No arbitrary fields.
BEGIN {
    count = split("name_client name_policy name_policy_full table_id table_mark custom_mark dscp_enable dscp_force_proxy dscp_force_proxy_tag dscp_exclude dscp_proxy ipv4_proxy ipv4_exclude ipv6_proxy ipv6_exclude proxy_dns proxy_router nfqws_mark pbr_strict start_verbose start_attempts init_delay udp_flush check_fd arm64_fd other_fd delay_fd ipv6_support extended_msg backup aghfix start_auto start_delay", order, " ")
    for (i = 1; i <= count; i++) declared[order[i]] = 1
    split("start_attempts init_delay arm64_fd other_fd delay_fd start_delay", numeric, " ")
    for (i in numeric) number[numeric[i]] = 1
    if (mode != "code" && mode != "settings") bad = 1
}
{
    # Retain every other line verbatim. Caller compares the normalized code;
    # undeclared/indented assignments therefore remain visible as code drift.
    line = $0
    key = line
    sub(/=.*/, "", key)
    if (key in declared && index(line, "=") > 0) {
        if (++seen[key] != 1 || length(line) > 4096) bad = 1
        value = substr(line, length(key) + 2)
        first = substr(value, 1, 1)
        last = substr(value, length(value), 1)
        quoted = length(value) >= 2 && (first == "\"" || first == "'") && last == first
        if (quoted) value = substr(value, 2, length(value) - 2)
        # One-line ASCII literals only, no shell expansions/escapes or embedded
        # quotes. Exact original assignment bytes are retained in settings.
        if (value ~ /[^ -~]/ || value ~ /["'\\$`]/) bad = 1
        if (key in number) {
            if (value !~ /^(0|[1-9][0-9]*)$/ || length(value) > 10) bad = 1
        } else if (!quoted) bad = 1
        settings[key] = line
        line = key "=NATIVE_DECLARED_SETTING"
    }
    code[NR] = line
}
END {
    for (i = 1; i <= count; i++) if (seen[order[i]] != 1) bad = 1
    # Refusal produces no partial projection that a caller could hash as valid.
    if (bad) exit 76
    if (mode == "settings") for (i = 1; i <= count; i++) print settings[order[i]]
    else for (i = 1; i <= NR; i++) print code[i]
}
