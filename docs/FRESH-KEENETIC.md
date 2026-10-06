# Fresh Keenetic installation

For the current native generation, use [quick start](QUICKSTART-RU.md) and [release installation](RELEASES.md).

1. Prepare supported `linux/arm64` Keenetic with working Entware `/opt` and private administrative access.
2. Install stock XKeen using its official installation procedure. It owns components, interception and cron. Select/configure the intended native mode and client policy; panel installation does not create firmware policy for you.
3. Verify ordinary Internet access and native configuration before adding the panel.
4. Install the release-specific signed panel alongside XKeen. No router Go/Node toolchain or blanket package upgrade.
5. Read the generated first panel password in the installer terminal. Default management is loopback8787; use a private SSH tunnel or explicitly configured trusted LAN listener.
6. Add subscriptions/nodes, validate and explicitly Apply config changes. Test the client path independently, not through an unrelated local VPN.
7. Save an encrypted configuration export; destination-native schedules/client policy are not silently overwritten by transfer.

Optional LAN DNS requires separately configured standard resolver and native Keenetic DNS profile. The panel does not install this infrastructure automatically. No legacy appliance adoption, takeover or native script patch is required.

Router reformat/reboot, unrelated `/opt` deletion, automatic credentials rotation and generic repair are not installation steps here. Prior legacy bootstrap/adoption evidence is retained [in archive](archive/FRESH-KEENETIC-before-0.3.1-cleanup.md). Installation of the current signed release on the operator router remains distinct from development delivery and public signature verification.
