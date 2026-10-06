# VPN profiles

Live provider profile strings are intentionally **not stored in Git**.

Historical versions tracked `main.txt` and `us.txt`. The secretless architecture removes them from the active branch. Production VPN material lives only in the router-local secret store described in `SECURITY.md` and ADR-001.

Historical local secret files may contain tags:

```text
proxy-main-01 ... proxy-main-10
proxy-us-01   ... proxy-us-03
```

These prefixes are historical, not current pool policy. Selection uses the native configuration and explicitly applied quality recommendation; legacy tag names do not imply that all nodes are current candidates.

The structured local registry at `/opt/etc/xkeen-control/secrets/nodes.json` removes this legacy distinction and renders canonical `proxy-<stable-id>` tags from validated VLESS/REALITY inputs. Credentials remain outside Git. Enabled subscriptions also refresh on schedule, retaining manual disabled state; see [node lifecycle](../docs/NODE-LIFECYCLE.md).
