#!/bin/sh
# Installs already-resolved distro package names into the guest. Invoked as
#   incus exec <instance> -- sh -s -- <manager> <package>...
# with this script piped over stdin, same as bootstrap-user.sh. Names
# arrive as separate argv entries and are forwarded as "$@" — never
# interpolated into a command string — so a name can't turn into shell
# syntax no matter what a profile contains.
#
# Name *resolution* deliberately happens on the host (internal/packages),
# not here: keeping the mapping table in Go makes it testable and keeps
# one source of truth, and this script stays small enough to read in one
# sitting.
set -eu

MGR="${1:?package manager required}"
shift

# Nothing to do is a success, not an error: it keeps the caller from
# needing to special-case an empty package list.
if [ "$#" -eq 0 ]; then
    exit 0
fi

case "$MGR" in
    apt)
        # Noninteractive or a postinst prompt hangs the create forever with
        # nothing on screen to explain why.
        DEBIAN_FRONTEND=noninteractive
        export DEBIAN_FRONTEND
        apt-get update -qq
        # --no-install-recommends: a sandbox should get what was asked for,
        # not a recommends closure that can pull in a mail transport agent.
        apt-get install -y -qq --no-install-recommends "$@"
        ;;
    dnf)
        dnf install -y --setopt=install_weak_deps=False "$@"
        ;;
    apk)
        apk add --no-cache "$@"
        ;;
    *)
        echo "agentctl: unsupported package manager \"$MGR\"" >&2
        exit 1
        ;;
esac
