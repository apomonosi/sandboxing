#!/bin/sh
# Idempotent non-root user bootstrap for a freshly created agentctl sandbox.
# Invoked as: incus exec <instance> -- sh -s -- <username>, with this
# script piped over stdin (see buildBootstrapUserArgs in translate.go) —
# never templated into a single command-line argument, to avoid any
# quoting hazards.
#
# Covers the two user-management ecosystems the images used throughout
# this project's docs/tests actually have: useradd (Debian/Ubuntu family)
# and adduser (Alpine/busybox family). On success, prints exactly one
# line of the form AGENTCTL_UID=<uid> so the caller can parse the
# resulting UID without needing a second round trip.
set -eu

USERNAME="${1:?username required}"
WANT_UID=1500

# Idempotent: re-running this after the user already exists is a no-op
# that just reports the existing UID (this is what makes the Part 2
# retry-on-next-start path safe to call repeatedly).
if id "$USERNAME" >/dev/null 2>&1; then
    echo "AGENTCTL_UID=$(id -u "$USERNAME")"
    exit 0
fi

# Prefer a fixed UID for predictability; fall back to whatever the OS
# assigns if it's already taken by a different account on this image.
UID_ARG=""
if command -v getent >/dev/null 2>&1; then
    if ! getent passwd "$WANT_UID" >/dev/null 2>&1; then
        UID_ARG="$WANT_UID"
    fi
else
    UID_ARG="$WANT_UID"
fi

ensure_sudo_includedir() {
    mkdir -p /etc/sudoers.d
    if [ -f /etc/sudoers ] && ! grep -q '^#includedir /etc/sudoers.d' /etc/sudoers 2>/dev/null; then
        echo '#includedir /etc/sudoers.d' >> /etc/sudoers
    fi
}

write_nopasswd_sudoers() {
    ensure_sudo_includedir
    echo "$USERNAME ALL=(ALL) NOPASSWD:ALL" > "/etc/sudoers.d/agentctl-$USERNAME"
    chmod 0440 "/etc/sudoers.d/agentctl-$USERNAME"
}

if command -v useradd >/dev/null 2>&1; then
    if [ -n "$UID_ARG" ]; then
        useradd -m -s /bin/bash -u "$UID_ARG" "$USERNAME"
    else
        useradd -m -s /bin/bash "$USERNAME"
    fi
    if ! command -v sudo >/dev/null 2>&1 && command -v apt-get >/dev/null 2>&1; then
        apt-get update -qq && apt-get install -y -qq sudo
    fi
    write_nopasswd_sudoers
elif command -v adduser >/dev/null 2>&1; then
    if [ -n "$UID_ARG" ]; then
        adduser -D -s /bin/sh -u "$UID_ARG" "$USERNAME"
    else
        adduser -D -s /bin/sh "$USERNAME"
    fi
    if ! command -v sudo >/dev/null 2>&1 && command -v apk >/dev/null 2>&1; then
        apk add --no-cache sudo
    fi
    write_nopasswd_sudoers
else
    echo "agentctl: neither useradd nor adduser found on this image; cannot provision a non-root user" >&2
    exit 1
fi

echo "AGENTCTL_UID=$(id -u "$USERNAME")"
