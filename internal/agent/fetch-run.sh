#!/bin/sh
# Installer download helper, prepended to the install script of every agent
# whose documented installer is a curl one-liner piped into a shell. Piped,
# the pipeline's status is the shell's, and a shell reading an empty script
# exits 0, so a download the egress policy refuses would be recorded as a
# successful install. This only defines a function; the caller runs
#
#   agentctl_fetch_run <agent> <shell> <url> [curl-flags...]
#
# which downloads <url> to a temporary file with the agent's own curl flags,
# fails with one "agentctl: <agent>: ..." line when curl does, and otherwise
# runs the file with <shell> and returns its status. The file is removed
# either way. The line names the HTTP status if there was one, and points
# at the egress policy only for a 403 (what agentctl's proxy answers) or a
# failed connection: a 429 sent to the allowlist is an hour lost. POSIX sh:
# Incus guests may be Alpine.

agentctl_fetch_run() {
	agentctl_fetch_agent=$1
	agentctl_fetch_shell=$2
	agentctl_fetch_url=$3
	shift 3
	agentctl_fetch_file=$(mktemp) || {
		echo "agentctl: $agentctl_fetch_agent: cannot create a temporary file for its installer" >&2
		return 1
	}
	# %{http_connect} is the proxy's answer to CONNECT: for an https URL,
	# that is where agentctl's egress proxy puts its 403, and %{http_code}
	# stays 000.
	agentctl_fetch_code=$(curl "$@" -o "$agentctl_fetch_file" -w '%{http_code} %{http_connect}' "$agentctl_fetch_url")
	agentctl_fetch_status=$?
	if [ "$agentctl_fetch_status" -ne 0 ]; then
		rm -f "$agentctl_fetch_file"
		agentctl_fetch_by="the server"
		case $agentctl_fetch_code in
		000\ 000 | "") agentctl_fetch_code= ;;
		000\ *)
			agentctl_fetch_code=${agentctl_fetch_code#* }
			agentctl_fetch_by="the proxy"
			;;
		*) agentctl_fetch_code=${agentctl_fetch_code%% *} ;;
		esac
		case $agentctl_fetch_code in
		403) echo "agentctl: $agentctl_fetch_agent: could not download $agentctl_fetch_url (HTTP 403, curl exit $agentctl_fetch_status); the sandbox's egress policy may be refusing it — check the agent's allow preset" >&2 ;;
		"") echo "agentctl: $agentctl_fetch_agent: could not download $agentctl_fetch_url: could not connect (curl exit $agentctl_fetch_status); check the network, or whether the sandbox's egress policy allows it — see the agent's allow preset" >&2 ;;
		[45]??) echo "agentctl: $agentctl_fetch_agent: could not download $agentctl_fetch_url: $agentctl_fetch_by refused the request with HTTP $agentctl_fetch_code (curl exit $agentctl_fetch_status)" >&2 ;;
		*) echo "agentctl: $agentctl_fetch_agent: could not download $agentctl_fetch_url (HTTP $agentctl_fetch_code, curl exit $agentctl_fetch_status)" >&2 ;;
		esac
		return "$agentctl_fetch_status"
	fi
	"$agentctl_fetch_shell" "$agentctl_fetch_file"
	agentctl_fetch_status=$?
	rm -f "$agentctl_fetch_file"
	return "$agentctl_fetch_status"
}
