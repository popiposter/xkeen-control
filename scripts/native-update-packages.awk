# Data-only projection of the supported native opkg status profile. Caller must
# protect/bound the input and parser, require final LF, and authenticate admission.
# Paragraph separators may change in native fixed_register_packages; stanza
# contents and order for unrelated packages must remain identical.
BEGIN {
    RS = ""
    if (mode != "other" && mode != "identity" && mode != "control") bad = 1
    if (version != "2.0.1") bad = 1 # Pinned public native profile only.
}
{
    if (NR > 512 || length($0) > 65536 || $0 ~ /[\000-\010\013-\037\177]/) bad = 1
    count = split($0, lines, "\n")
    if (lines[1] !~ /^Package: [A-Za-z0-9][A-Za-z0-9+_.-]*$/) bad = 1
    package = substr(lines[1], 10)
    if (++seen[package] != 1) bad = 1
    # Native delete_register_xkeen matches this prefix, not the full name.
    if (package ~ /^xkeen/ && package != "xkeen") bad = 1
    for (key in fields) delete fields[key]
    previous = ""
    for (i = 1; i <= count; i++) {
        if (length(lines[i]) > 8192) bad = 1
        if (lines[i] ~ /^[ \t]/) {
            if (previous == "" || package == "xkeen") bad = 1
            continue
        }
        if (lines[i] !~ /^[A-Za-z][A-Za-z0-9-]*: /) {bad = 1; continue}
        key = lines[i]; sub(/:.*/, "", key)
        if (key in fields) bad = 1
        fields[key] = substr(lines[i], length(key) + 3)
        previous = key
        if (key == "Architecture" && !first_architecture_seen) {
            first_architecture_seen = 1
            first_architecture = fields[key]
        }
    }
    if (package == "xkeen") {
        own++
        if (fields["Version"] != version ||
            fields["Depends"] != "jq, curl, coreutils-uname, coreutils-nohup, iptables, ipset, ip-full, conntrack" ||
            fields["Architecture"] !~ /^[A-Za-z0-9][A-Za-z0-9_.-]*$/) bad = 1
        architecture = fields["Architecture"]
        if (mode == "control") {
            if (count != 11 || fields["Source"] != "Skrill" || fields["SourceName"] != "xkeen" ||
                fields["Section"] != "net" || fields["Maintainer"] != "Skrill / jameszero" ||
                fields["Description"] != "The platform that makes Xray work." ||
                fields["SourceDateEpoch"] !~ /^(0|[1-9][0-9]*)$/ || length(fields["SourceDateEpoch"]) > 10 ||
                fields["Installed-Size"] !~ /^(0|[1-9][0-9]*)$/ || length(fields["Installed-Size"]) > 10) bad = 1
            allowed = " Package Version Depends Source SourceName Section SourceDateEpoch Maintainer Architecture Installed-Size Description "
        } else {
            if (count != 6 || fields["Status"] != "install user installed" ||
                fields["Installed-Time"] !~ /^(0|[1-9][0-9]*)$/ || length(fields["Installed-Time"]) > 10) bad = 1
            allowed = " Package Version Depends Status Architecture Installed-Time "
        }
        for (key in fields) if (!index(allowed, " " key " ")) bad = 1
    } else {
        other[++others] = $0
    }
}
END {
    # Native derives registration architecture from the first status stanza.
    if (own != 1 || first_architecture != architecture || (mode == "control" && NR != 1)) bad = 1
    if (bad) exit 76 # Never emit partial output that could be mistaken for proof.
    if (mode == "identity" || mode == "control") {
        # Native registration timestamps/size vary; declarative identity does not.
        print "xkeen " version " " architecture
    } else for (i = 1; i <= others; i++) printf "%s\n\n", other[i]
}
