#!/bin/sh
set -eu

env_file=/srv/data/projecthub/.env
lan_ip=$(/usr/bin/awk -F= '$1 == "LAN_BIND_IP" { print $2; exit }' "$env_file")
case "$lan_ip" in
	''|*[!0-9.]*)
		/usr/bin/logger -t projecthub "LAN_BIND_IP is missing or invalid in $env_file"
		exit 1
		;;
esac

attempt=0
while [ "$attempt" -lt 180 ]; do
	if /usr/sbin/ip -4 -o address show | /usr/bin/awk -v wanted="$lan_ip" '{ split($4, cidr, "/"); if (cidr[1] == wanted) found=1 } END { exit !found }'; then
		exit 0
	fi
	attempt=$((attempt + 1))
	/bin/sleep 1
done

/usr/bin/logger -t projecthub "LAN address $lan_ip was not assigned within 180 seconds; refusing wildcard publication"
exit 1
