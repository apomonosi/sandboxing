package lima

import (
	"fmt"
	"path/filepath"
)

// Other ways Lima reaches from the guest back to the host
//
// Two more Lima settings open a channel from the guest to the host, and
// agentctl has a use for neither:
//
//   - a portForwards entry with reverse: true forwards a host unix socket
//     into the guest (`ssh -R`), so anything in the guest can talk to
//     whatever listens on it — the host's Docker daemon (root on the host,
//     in effect), say, or its GPG agent;
//   - a copyToHost entry has Lima copy a guest file to a host path once
//     the guest is up, so anything in the guest decides what's written
//     there. That's not safe even inside the instance's own directory: a
//     lima.yaml written there is what checkMounts trusts as the instance's
//     own configuration.
//
// Lima merges both from $LIMA_HOME/_config/default.yaml and override.yaml
// into every instance it loads, the way it merges mounts, and a template
// can declare them too: Lima's Kubernetes templates (k3s, k8s, ...) copy a
// kubeconfig out of the guest, for the host's kubectl to use. create
// clears copyToHost, as it does mounts (see buildSetExpressions); no stock
// template declares a reverse forward. And since agentctl never asks for
// either, Start refuses an instance whose effective configuration has
// any, whatever declared it. (Verified against Lima v2.2.1: FillDefault in
// pkg/limayaml/defaults.go, and forwardSSH and copyToHost in
// pkg/hostagent.)

// checkHostChannels returns an error describing the first reverse socket
// forward or copyToHost entry in j's effective configuration, or nil if
// it has neither.
func checkHostChannels(j limaInstanceJSON) error {
	if j.Config == nil {
		return fmt.Errorf("could not read Lima's effective configuration for %q to check whether it would forward host sockets into it or copy files out of it", j.Name)
	}
	cfgDir := j.configDir()
	defaultYAML, overrideYAML := filepath.Join(cfgDir, "default.yaml"), filepath.Join(cfgDir, "override.yaml")
	for _, pf := range j.Config.PortForwards {
		if !pf.Reverse {
			continue
		}
		return fmt.Errorf("Lima would forward the host socket %s into instance %q (at %s), so agentctl won't start it: anything in the guest could use whatever listens on it. "+
			"Lima merges the portForwards in %s and %s into every instance: remove the entry there. "+
			"If neither file has it, remove it from the instance's own configuration: "+
			"`limactl edit --set 'del(.portForwards[] | select(.reverse == true))' --start=false %s`",
			pf.HostSocket, j.Name, pf.GuestSocket, defaultYAML, overrideYAML, j.Name)
	}
	if len(j.Config.CopyToHost) > 0 {
		c := j.Config.CopyToHost[0]
		return fmt.Errorf("Lima would copy %s from instance %q to %s on the host, so agentctl won't start it: anything in the guest could decide what's written there. "+
			"Lima merges the copyToHost entries in %s and %s into every instance: remove the entry there. "+
			"If neither file has it, clear them in the instance's own configuration, which an older agentctl, or limactl, may have created with them: "+
			"`limactl edit --set '.copyToHost = []' --start=false %s`",
			c.GuestFile, j.Name, c.HostFile, defaultYAML, overrideYAML, j.Name)
	}
	return nil
}
