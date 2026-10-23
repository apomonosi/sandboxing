#!/bin/sh
# PATH helper, prepended to every agent's install script. It only defines
# functions; after the agent's own installer has succeeded, the caller runs
#
#   agentctl_agent_on_path <agent> <binary>
#
# which makes the agent runnable by name: a no-op when <binary> is already
# on PATH, and otherwise puts the first candidate directory holding it on
# PATH, now and through ~/.profile. It never fails the install; a binary
# found nowhere only gets one "agentctl: <agent>: ..." line. node-bootstrap.sh
# uses agentctl_path_add from here too. POSIX sh: Incus guests may be Alpine.

# agentctl_path_add DIR puts $HOME/DIR first on PATH for the rest of this
# script, and adds the same export to ~/.profile once, since login shells
# (agentctl shell) read it and agentctl's exec does not.
agentctl_path_add() {
	# shellcheck disable=SC2016 # expanded by the login shell, not here
	agentctl_path_put "$HOME/$1" '$HOME/'"$1"
}

# agentctl_path_put DIR SPELLING does agentctl_path_add's work for DIR,
# writing it in ~/.profile as SPELLING.
agentctl_path_put() {
	PATH=$1:$PATH
	export PATH
	# shellcheck disable=SC2016 # expanded by the login shell, not here
	export_line='export PATH="'$2':$PATH"'
	grep -qxF "$export_line" "$HOME/.profile" 2>/dev/null ||
		printf '%s\n' "$export_line" >>"$HOME/.profile"
}

# agentctl_agent_on_path AGENT BINARY runs after AGENT's own installer.
# Installers put their binary somewhere of their own choosing and often
# add it to no PATH a later `agentctl shell` reads, so this looks in the
# places they use rather than knowing each one's.
agentctl_agent_on_path() {
	command -v "$2" >/dev/null 2>&1 && return 0
	for agentctl_path_dir in .local/bin ".$1/bin" ".$1/agent/bin"; do
		if [ -x "$HOME/$agentctl_path_dir/$2" ]; then
			agentctl_path_add "$agentctl_path_dir"
			return 0
		fi
	done
	if [ -x "/usr/local/bin/$2" ]; then
		agentctl_path_put /usr/local/bin /usr/local/bin
		return 0
	fi
	echo "agentctl: $1: no $2 on PATH or in ~/.local/bin, ~/.$1/bin, ~/.$1/agent/bin or /usr/local/bin after its installer, so it is not on PATH; add the directory it installed to by hand" >&2
	return 0
}
