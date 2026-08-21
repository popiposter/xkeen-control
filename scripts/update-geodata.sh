#!/bin/sh
set -eu
DEST="/opt/etc/xray/dat"
mkdir -p "$DEST"
fetch() {
  name="$1"; url="$2"; tmp="$DEST/.$name.tmp.$$"
  echo "Updating $name"
  if curl -fL --connect-timeout 10 -m 90 -o "$tmp" "$url"; then :
  elif curl -fL --connect-timeout 10 -m 90 -o "$tmp" "https://gh-proxy.com/$url"; then :
  elif curl -fL --connect-timeout 10 -m 90 -o "$tmp" "https://ghfast.top/$url"; then :
  else rm -f "$tmp"; echo "ERROR: failed to download $name" >&2; exit 1; fi
  [ -s "$tmp" ] || { rm -f "$tmp"; echo "ERROR: empty $name" >&2; exit 1; }
  mv "$tmp" "$DEST/$name"
}
fetch geosite_refilter.dat "https://github.com/1andrevich/Re-filter-lists/releases/latest/download/geosite.dat"
fetch geoip_refilter.dat   "https://github.com/1andrevich/Re-filter-lists/releases/latest/download/geoip.dat"
fetch geosite_v2fly.dat    "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat"
fetch geoip_v2fly.dat      "https://github.com/loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat"
fetch geosite_zkeen.dat    "https://github.com/jameszeroX/zkeen-domains/releases/latest/download/zkeen.dat"
fetch geoip_zkeenip.dat    "https://github.com/jameszeroX/zkeen-ip/releases/latest/download/zkeenip.dat"
echo "Geo data updated in $DEST"
