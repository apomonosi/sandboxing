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

// parseMountFlag parses a "--mount hostPath[:guestPath][:w|:ro]" value.
//
// Parsed right to left, because a Windows host path carries its own colon
// ("C:\Users\me\src") and splitting left to right would mistake the drive
// letter for a path component. Two markers make that unambiguous:
//
//   - the writability suffix is only ever the literal "w" or "ro", so any
//     other trailing token is part of a path;
//   - a guest path is always an absolute *Linux* path — agentctl guests
//     are Linux on every backend — so it always starts with "/", which a
//     Windows drive letter never does.
//
// Omitting the guest path leaves it empty here; ResolveMounts defaults it
// to the host path, matching Lima's "mountPoint builtin default: value of
// location". Omitting the suffix means read-only, matching Lima's
// writable default of false.
func parseMountFlag(s string) (profile.Mount, error) {
	spec := strings.TrimSpace(s)
	if spec == "" {
		return profile.Mount{}, fmt.Errorf("invalid --mount %q: host path must not be empty", s)
	}

	var writable bool
	if i := strings.LastIndex(spec, ":"); i != -1 {
		switch spec[i+1:] {
		case "w":
			writable, spec = true, spec[:i]
		case "ro":
			writable, spec = false, spec[:i]
		}
	}

	var guestPath string
	if i := strings.LastIndex(spec, ":"); i != -1 && strings.HasPrefix(spec[i+1:], "/") {
		guestPath, spec = spec[i+1:], spec[:i]
	}

	if spec == "" {
		return profile.Mount{}, fmt.Errorf("invalid --mount %q: host path must not be empty", s)
	}
	return profile.Mount{HostPath: spec, GuestPath: guestPath, Writable: writable}, nil
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
