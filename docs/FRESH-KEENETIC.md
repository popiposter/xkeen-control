# Fresh Keenetic installation

The supported order is **Keenetic/Entware → official XKeen/Xray → signed panel alongside → optional independent DNS → configuration/subscriptions → explicit activation**. The panel's current installer requires stock XKeen first. See [quick start](QUICKSTART-RU.md), [release installation](RELEASES.md) and the proposed [guided setup plan](../plan/feature-fresh-router-setup-1.md).

1. Prepare supported `linux/arm64` Keenetic with working Entware `/opt` and private administrative access.
2. Install stock XKeen using its official installation procedure. It owns components, interception and cron. Select/configure the intended native mode and client policy; panel installation does not create firmware policy for you.
3. Verify ordinary direct Internet and inspect the native configuration before adding the panel. An active VPN subscription is not required at this step; keep incomplete native proxy configuration stopped.
4. Install the release-specific signed panel alongside XKeen. No router Go/Node toolchain or blanket package upgrade.
5. Read the generated first panel password in the installer terminal. Default management is loopback8787; use a private SSH tunnel or explicitly configured trusted LAN listener.
6. Add subscriptions/nodes, validate and explicitly Apply config changes. Test the client path independently, not through an unrelated local VPN.
7. Save an encrypted configuration export; destination-native schedules/client policy are not silently overwritten by transfer.

Optional LAN DNS requires separately configured standard resolver and native Keenetic DNS profile. The panel does not install this infrastructure automatically. No legacy appliance adoption, takeover or native script patch is required.

## Entware tools and the official quick launcher

Prepare direct Internet/DNS, ARM64 Entware, free storage and private management first. On a **fresh** destination install the standard prerequisites, without a blanket upgrade:

```sh
export PATH="/opt/bin:/opt/sbin:${PATH:-/usr/sbin:/usr/bin:/sbin:/bin}"
opkg update
opkg install curl tar jq
command -v tar
tar --version
```

The selected tar must be GNU tar from `/opt/bin`; installing the package alone is insufficient if `/opt/usr/bin/tar` (BusyBox) wins PATH. The source correction in #142 is not yet published in0.3.1; inspect the installed panel environment independently. Official XKeen handles its remaining dependencies. No Go/Node runs on the router.

On firmware5.2, prepare the private RCI token in protected `/opt/etc/xkeen/xkeen.json` **before launching bootstrap**, following [upstream instructions](https://github.com/jameszeroX/XKeen/wiki/Порядок-установки). Native initialization can exit on401/403 before reaching the installation menu. Token creation is an explicit administrator action; older firmware does not acquire this requirement automatically.

The official interactive bootstrap can be pinned to the verified2.1 release on an empty destination:

```sh
curl -fsSL https://raw.githubusercontent.com/jameszeroX/XKeen/2.1/install.sh -o /tmp/xkeen-install.sh
sh /tmp/xkeen-install.sh --legacy 2.1
```

Run the second line only after successful download. This is not an existing-router update/reinstall procedure. Tagged2.1's launcher **attempts** GitHub asset-digest verification but can continue with a warning when `jq`, the API/digest or `sha256sum` is unavailable. Install `jq` beforehand, check SHA-256 tooling/API availability and inspect the actual verification result; a skipped check remains NOT VERIFIED, never an integrity PASS. Independently verify the release digest/payload before accepting the installation. Installed version readback alone does not replace that check. `--stable` instead selects the latest stable at execution time and needs fresh compatibility review if that version changes.

Choose **Xray only** in its native menu. Review the core version instead of assuming the latest entry is stable/qualified. Install geodata referenced by the selected policy, choose native schedule/autostart explicitly and keep clients on ordinary direct Internet while configuration is incomplete.

The documented `xkeen -i auto cores=xray` assumes the dispatcher already exists. Tagged2.1 `install.sh` ends with interactive `xkeen -i` and does not forward `auto`/core arguments. Do not advertise `install.sh --stable cores=xray` as a fully automatic fresh installation or run a second full installation to automate the first. A future zero-question flow needs a supported upstream bootstrap interface.

## Firmware client policy

For the recommended selective-client mode create an Internet access policy displayed as `xkeen`, select usable WAN connections and explicitly choose clients/segment. Prepare assignments, then activate them after the proxy/DNS path is ready. A policy identifies clients; it is not a VPN tunnel. Do not alter the default policy or move every client implicitly. A specifically named policy is not mandatory for every other native operating mode.

Creation is possible in both GUI and official **Keenetic CLI**: `ip policy <id>`, `description xkeen`, destination-specific WAN/client assignments and configuration save. These are firmware console commands, not Entware shell commands. Select a free id; never assume `Policy0` or copy another router's interfaces/marks. GUI is the initial supported provisioning route here; the [CLI reference](https://storage.googleapis.com/docs.help.keenetic.com/cli/5.0/en/cli_manual_kn-1811.pdf) documents the alternative.

Read-only `show ip policy` or fixed RCI `show/ip/policy` can confirm existence. Stock2.1 matches the **description** case-insensitively and reads its mark; the internal id may remain `PolicyN`. Verify exactly one match, valid mark, usable WAN and actual intended client/segment assignments. Timeout/unauthorized/malformed response means unknown, not absent. Firmware5.2 token stays private. A new typed panel preflight/writer is not implemented; [Issue143](https://github.com/popiposter/xkeen-control/issues/143) specifies the read-only slice first.

## Panel, independent DNS and reference configuration

Install the signed panel after native components and use its supported native attachment/config workflow. Management defaults to loopback8787/private SSH tunnel. Standard mosdns is separate optional infrastructure: verified ARM64 binary, fixed `S06mosdns`, protected `/opt/etc/mosdns` and RAM logs/cache, no second Xray process. Configure/test it before selecting the firmware DNS profile. Retain the original profile for explicit rollback.

Firmware retains LAN53/local names. Current panel readiness expects `127.0.0.1:15354`; firmware forwarding needs a proven loopback path or one exact destination trusted LAN address, never wildcard/WAN. DIRECT DoH is independent of Xray; VPN DoH follows loopback SOCKS/native pool without DIRECT fallback. Inspect host overrides and native interception of53; correct it through supported excluded-port commands if required. That command can already restart Xray, so avoid a redundant restart. [Current DNS integration](NATIVE-XKEEN.md#optional-independent-lan-dns-integration-issue-125) supersedes the static exports in the [historical infrastructure evidence](../plan/infrastructure-independent-dns-1.md); that old operation is not an installer to replay.

The proposed [RU selective v1 reference](../config/presets/README.md) has DIRECT default, selected VPN services, scoped BLOCK and BitTorrent DIRECT outside force-proxy. It contains public traffic policy only, no node ids/weights/credentials, service integration or firmware bindings. This is a **fragment**, not a complete replacement config or an implemented UI preset. A future explicit Preview must merge destination integration and validate all tags/geodata. Do not copy it over a working config.

After preparing the profile/own configuration, add subscriptions, verify enabled pool/BLOCK fallback, validate the complete candidate and explicitly Save/Apply once. Independently inspect native process/executable/config/API/probe and DNS synchronization. Test DIRECT, selected VPN, local names and cold DNS/failure/recovery from an unproxied LAN client. Exit0/downloads/policy presence are not acceptance. Encrypted export afterward remains portable private state, not a public reference snapshot.

Router reformat/reboot, unrelated `/opt` deletion, automatic credentials rotation and generic repair are not installation steps here. Prior legacy bootstrap/adoption evidence is retained [in archive](archive/FRESH-KEENETIC-before-0.3.1-cleanup.md). Installation of the current signed release on the operator router remains distinct from development delivery and public signature verification.
