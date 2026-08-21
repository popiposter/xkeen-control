# VPN profiles

Live provider profile strings are intentionally **not stored in Git**.

Historical versions tracked `main.txt` and `us.txt`. The secretless architecture removes them from the active branch. Production VPN material lives only in the router-local secret store described in `SECURITY.md` and ADR-001.

The current local secret file may still contain historical tags:

```text
proxy-main-01 ... proxy-main-10
proxy-us-01   ... proxy-us-03
```

These prefixes no longer define separate pools. `bal-proxy` selects both families and treats all nodes as equal candidates.

The structured local registry at `/opt/etc/xkeen-control/secrets/nodes.json` removes this legacy distinction and renders canonical `proxy-<stable-id>` tags from strict VLESS/REALITY inputs. Credentials remain outside Git; subscription refresh is explicit and preview-first.
