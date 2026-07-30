package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/apomonosi/sandboxing/internal/profile"
)

// parsePortFlag parses a Docker-style "--port host:guest[/proto]" value.
func parsePortFlag(s string) (profile.PortPublish, error) {
	proto := "tcp"
	spec := s
	if i := strings.LastIndex(s, "/"); i != -1 {
		proto = s[i+1:]
		spec = s[:i]
	}
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) != 2 {
		return profile.PortPublish{}, fmt.Errorf("invalid --port %q: want host:guest[/proto]", s)
	}
	host, err := strconv.Atoi(parts[0])
	if err != nil {
		return profile.PortPublish{}, fmt.Errorf("invalid --port %q: host port %q is not a number", s, parts[0])
	}
	guest, err := strconv.Atoi(parts[1])
	if err != nil {
		return profile.PortPublish{}, fmt.Errorf("invalid --port %q: guest port %q is not a number", s, parts[1])
	}
	return profile.PortPublish{Host: host, Guest: guest, Protocol: proto}, nil
}

// parseAllowFlag parses a "--allow domain[:port,port,...]" value.
func parseAllowFlag(s string) (profile.AllowRule, error) {
	parts := strings.SplitN(s, ":", 2)
	rule := profile.AllowRule{Domain: parts[0]}
	if rule.Domain == "" {
		return profile.AllowRule{}, fmt.Errorf("invalid --allow %q: domain must not be empty", s)
	}
	if len(parts) == 2 {
		for _, p := range strings.Split(parts[1], ",") {
			port, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil {
				return profile.AllowRule{}, fmt.Errorf("invalid --allow %q: port %q is not a number", s, p)
			}
			rule.Ports = append(rule.Ports, port)
		}
	}
	return rule, nil
}
