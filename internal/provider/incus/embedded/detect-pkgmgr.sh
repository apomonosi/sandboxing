#!/bin/sh
# Reports which package manager the guest has, so the host side can map
# agentctl's neutral tool names onto that distribution's real package
# names (see internal/packages). Invoked as:
#   incus exec <instance> -- sh -s
# with this script piped over stdin, same as bootstrap-user.sh.
#
# Prints exactly one line, AGENTCTL_PKGMGR=<name>, and always exits 0 —
# including when nothing is found, where it prints an empty value. An
# unrecognized guest is a condition the caller reports with a useful
# message naming the distributions agentctl drives; it is not a script
# failure, and surfacing it as one would bury that message under a raw
# "exited 1".
set -eu

if command -v apt-get >/dev/null 2>&1; then
    echo "AGENTCTL_PKGMGR=apt"
    exit 0
fi

# dnf before yum: on RHEL-family images that still ship a `yum` shim, it
# is a symlink to dnf anyway, and dnf is the name worth reporting.
if command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
    echo "AGENTCTL_PKGMGR=dnf"
    exit 0
fi

if command -v apk >/dev/null 2>&1; then
    echo "AGENTCTL_PKGMGR=apk"
    exit 0
fi

echo "AGENTCTL_PKGMGR="
