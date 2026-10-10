package lima

import (
	"fmt"
	"path/filepath"
)

// Host access Lima forwards behind agentctl's back
//
// Lima can forward two things from the host into a guest over SSH: the
// host's SSH agent (ssh.forwardAgent) and its X11 display
// (ssh.forwardX11). Both are off by default, and a sandbox should get
// neither. With the agent, anything in the guest can authenticate as the
// user wherever their keys are accepted — push to their GitHub
// repositories, log in to their servers — without ever reading a key
// file. Lima forwards it not only through `limactl shell`, which
// agentctl's shell and exec run, but through the hostagent's own SSH
// connection, linking it at /run/host-services/ssh-auth.sock in the
// guest for as long as the instance runs. X11 is out for the reason
// docs/admin/security-model.md gives: an X server doesn't isolate its
// clients from each other.
//
// buildSetExpressions turns both off in the instance's own lima.yaml.
// That beats $LIMA_HOME/_config/default.yaml, where many people turn
// agent forwarding on for all their Lima VMs, but not override.yaml,
// which beats every instance's own setting; and an instance an older
// agentctl created, or limactl did, may not turn them off at all. So,
// like checkMounts, Start checks the effective configuration before every
// start, and refuses rather than warns. (Verified against Lima v2.2.1:
// FillDefault in pkg/limayaml/defaults.go, sshutil.SSHOpts, which both
// `limactl shell` and the hostagent use, and the hostagent's handling of
// ssh.forwardAgent.)

// checkForwarding returns an error if j's effective configuration
// forwards the host's SSH agent or X11 display into the guest, or nil if
// it forwards neither.
func checkForwarding(j limaInstanceJSON) error {
	if j.Config == nil {
		return fmt.Errorf("could not read Lima's effective configuration for %q to check whether it would forward the host's SSH agent or X11 display", j.Name)
	}
	for _, f := range []struct {
		on        bool
		field     string
		forwarded string
		risk      string
	}{
		{j.Config.SSH.ForwardAgent, "forwardAgent", "the host's SSH agent", "anything in the guest could use it to authenticate as you"},
		{j.Config.SSH.ForwardX11, "forwardX11", "the host's X11 display", "anything in the guest could connect to your X server"},
	} {
		if !f.on {
			continue
		}
		return fmt.Errorf("Lima would forward %s into instance %q, so agentctl won't start it: %s. "+
			"agentctl creates instances with ssh.%s off, but %s overrides that: remove it there. "+
			"If that file doesn't set it, turn it off in the instance's own configuration, which an older agentctl, or limactl, may have created without it: "+
			"`limactl edit --set '.ssh.%s = false' --start=false %s`",
			f.forwarded, j.Name, f.risk, f.field, filepath.Join(j.configDir(), "override.yaml"), f.field, j.Name)
	}
	return nil
}
