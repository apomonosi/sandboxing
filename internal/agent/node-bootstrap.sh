#!/bin/sh
# Node.js bootstrap, prepended to the install script of an agent whose own
# installer needs Node but cannot install it without a terminal (pi). It
# only defines functions; the caller runs
#
#   agentctl_ensure_node <agent> <minimum-version>
#
# which is a no-op when a new enough node is already on PATH, and otherwise
# installs Node system-wide (or under ~/.local/node without sudo). Every
# failure prints one "agentctl: <agent>: ..." line and exits non-zero, so
# agentctl's retry-on-start applies. Installing without sudo uses
# agentctl_path_add, so path-add.sh must come first. POSIX sh: Incus
# guests may be Alpine.

# The Node release line installed when the guest has none, or too old a
# one. pi's floor is 22.19.0, which every current v22 LTS release meets.
agentctl_node_line=latest-v22.x
agentctl_node_dist=https://nodejs.org/dist/$agentctl_node_line

agentctl_node_fail() {
	echo "agentctl: $agentctl_node_agent: $*" >&2
	exit 1
}

# agentctl_version_ge A B succeeds when dotted version A (an optional
# leading "v", then major.minor.patch) is at least B, comparing each part
# as a number: 22.100.0 is newer than 22.19.0, 9.0.0 older than 22.0.0.
agentctl_version_ge() {
	a=${1#v}
	b=${2#v}
	for _ in 1 2 3; do
		ah=${a%%.*}
		bh=${b%%.*}
		ah=${ah%%[!0-9]*}
		bh=${bh%%[!0-9]*}
		case $a in *.*) a=${a#*.} ;; *) a=0 ;; esac
		case $b in *.*) b=${b#*.} ;; *) b=0 ;; esac
		[ -n "$ah" ] || return 1
		[ -n "$bh" ] || bh=0
		[ "$ah" -gt "$bh" ] && return 0
		[ "$ah" -lt "$bh" ] && return 1
	done
	return 0
}

# agentctl_node_ok succeeds when node is on PATH and at least the floor.
agentctl_node_ok() {
	agentctl_node_found=
	command -v node >/dev/null 2>&1 || return 1
	agentctl_node_found=$(node -v 2>/dev/null) || return 1
	agentctl_version_ge "$agentctl_node_found" "$agentctl_node_min"
}

# Alpine is musl; nodejs.org's Linux builds are glibc, so take the
# distribution's package and check that it is new enough.
agentctl_node_install_apk() {
	sudo -n true 2>/dev/null ||
		agentctl_node_fail "installing Node.js on Alpine needs sudo for apk add"
	sudo -n apk add --no-cache nodejs npm ||
		agentctl_node_fail "apk add nodejs npm failed"
}

# glibc guests: the latest release of agentctl_node_line from nodejs.org,
# checked against the release's SHASUMS256.txt, unpacked into /usr/local
# (or ~/.local/node when there is no passwordless sudo).
agentctl_node_install_tarball() {
	case $(uname -m) in
	x86_64 | amd64) arch=x64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) agentctl_node_fail "no Node.js build for architecture $(uname -m)" ;;
	esac
	command -v curl >/dev/null 2>&1 || agentctl_node_fail "curl is not installed"
	command -v sha256sum >/dev/null 2>&1 || agentctl_node_fail "sha256sum is not installed"
	# tar shells out to xz for .tar.xz; without it, take the .tar.gz build.
	if command -v xz >/dev/null 2>&1; then
		ext=tar.xz
		unpack=-xJf
	else
		ext=tar.gz
		unpack=-xzf
	fi
	major=${agentctl_node_line#latest-v}
	major=${major%.x}

	tmp=$(mktemp -d) || agentctl_node_fail "cannot create a temporary directory"
	curl -fsSL "$agentctl_node_dist/SHASUMS256.txt" -o "$tmp/SHASUMS256.txt" ||
		agentctl_node_fail "downloading $agentctl_node_dist/SHASUMS256.txt failed"
	line=$(grep " node-v$major\.[0-9.]*-linux-$arch\.$ext\$" "$tmp/SHASUMS256.txt" | head -n 1)
	[ -n "$line" ] ||
		agentctl_node_fail "no linux-$arch .$ext build in $agentctl_node_dist/SHASUMS256.txt"
	file=${line##* }
	curl -fsSL "$agentctl_node_dist/$file" -o "$tmp/$file" ||
		agentctl_node_fail "downloading $agentctl_node_dist/$file failed"
	(cd "$tmp" && printf '%s\n' "$line" | sha256sum -c - >/dev/null 2>&1) ||
		agentctl_node_fail "checksum mismatch for $file"

	if sudo -n true 2>/dev/null; then
		# --no-same-owner: as root, tar would otherwise keep the archive's
		# uid, leaving /usr/local writable by whichever guest user has it.
		sudo -n tar "$unpack" "$tmp/$file" -C /usr/local --strip-components=1 --no-same-owner ||
			agentctl_node_fail "unpacking $file into /usr/local failed"
		case :$PATH: in
		*:/usr/local/bin:*) ;;
		*) PATH=/usr/local/bin:$PATH && export PATH ;;
		esac
	else
		prefix=$HOME/.local/node
		{ mkdir -p "$prefix" && tar "$unpack" "$tmp/$file" -C "$prefix" --strip-components=1; } ||
			agentctl_node_fail "unpacking $file into $prefix failed"
		agentctl_path_add .local/node/bin
	fi
	rm -rf "$tmp"
}

agentctl_ensure_node() {
	agentctl_node_agent=$1
	agentctl_node_min=$2
	agentctl_node_ok && return 0
	if [ -f /etc/alpine-release ]; then
		agentctl_node_install_apk
	else
		agentctl_node_install_tarball
	fi
	hash -r 2>/dev/null
	agentctl_node_ok && return 0
	if [ -n "$agentctl_node_found" ]; then
		agentctl_node_fail "installed Node.js is $agentctl_node_found, but $agentctl_node_agent needs $agentctl_node_min or newer"
	fi
	agentctl_node_fail "node is still not on PATH after installing Node.js"
}
