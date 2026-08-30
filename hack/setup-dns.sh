#!/usr/bin/env bash
# One-time host setup so `*.localhost` dev URLs resolve to 127.0.0.1 for ALL
# clients, not just browsers that special-case it. Linux + systemd-resolved:
# no-op. macOS: dnsmasq wildcard resolver for the localhost TLD.
set -euo pipefail

tld="${DEV_TLD:-localhost}"

if [[ "$(uname)" == "Darwin" ]]; then
	if dscacheutil -q host -a name "dev-env-probe.$tld" 2>/dev/null | grep -q .; then
		echo "*.$tld already resolves; nothing to do."
		exit 0
	fi
	command -v brew > /dev/null || { echo "install Homebrew first" >&2; exit 1; }
	brew list dnsmasq > /dev/null 2>&1 || brew install dnsmasq
	conf="$(brew --prefix)/etc/dnsmasq.conf"
	grep -q "^address=/$tld/" "$conf" 2>/dev/null || echo "address=/$tld/127.0.0.1" >> "$conf"
	sudo brew services start dnsmasq
	sudo mkdir -p /etc/resolver
	printf 'nameserver 127.0.0.1\n' | sudo tee "/etc/resolver/$tld" > /dev/null
	echo "dnsmasq wildcard for *.$tld installed (one-time)."
else
	if getent hosts "dev-env-probe.$tld" > /dev/null 2>&1; then
		echo "*.$tld already resolves (systemd-resolved); nothing to do."
	else
		echo "WARNING: *.$tld does not resolve. Browsers still special-case *.localhost;" >&2
		echo "for curl/other tools add a wildcard resolver or set DEV_TLD." >&2
		exit 1
	fi
fi
