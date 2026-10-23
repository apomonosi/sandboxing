package profile

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ParseAllowEntry parses one egress allowlist entry, as written in an
// `--allow` flag or on a line of an allow file (--allow-file, allowFile):
//
//	example.com            any TCP port
//	example.com:443        one port
//	example.com:80,443     several ports
//	*.example.com:443      a wildcard (see the AllowRule docs for how each
//	                       backend matches one)
//	203.0.113.7:443        an IPv4 address
//	[2001:db8::1]:443      an IPv6 address (the brackets are only needed
//	                       when giving ports)
//	https://example.com    a URL's host, on its port: the one the URL
//	                       names, else 443 for https and 80 for http
//
// Egress is filtered by host and port, never by URL path, so a URL with a
// path is rejected rather than quietly widened to its whole host.
func ParseAllowEntry(s string) (AllowRule, error) {
	entry := strings.TrimSpace(s)
	if entry == "" {
		return AllowRule{}, errors.New("empty entry")
	}
	if strings.Contains(entry, "://") {
		return parseAllowURL(entry)
	}

	var host, ports string
	hasPorts := false
	switch {
	case strings.HasPrefix(entry, "["):
		end := strings.Index(entry, "]")
		if end < 0 {
			return AllowRule{}, errors.New("missing \"]\" after an IPv6 address")
		}
		host, ports = entry[1:end], entry[end+1:]
		if net.ParseIP(host) == nil {
			return AllowRule{}, fmt.Errorf("%q in brackets is not an IPv6 address", host)
		}
		if ports != "" {
			var ok bool
			if ports, ok = strings.CutPrefix(ports, ":"); !ok {
				return AllowRule{}, fmt.Errorf("unexpected %q after the IPv6 address (want [address]:ports)", ports)
			}
			hasPorts = true
		}
	case net.ParseIP(entry) != nil:
		host = entry // an IP address, IPv6 included, with no ports
	default:
		host, ports, hasPorts = strings.Cut(entry, ":")
	}

	if strings.Contains(host, ",") {
		return AllowRule{}, errors.New("one host per entry (commas separate ports, as in example.com:80,443)")
	}
	host = strings.ToLower(host)
	if err := checkDomain(host); err != nil {
		return AllowRule{}, err
	}
	rule := AllowRule{Domain: host}
	if hasPorts {
		for _, p := range strings.Split(ports, ",") {
			port, err := parsePort(p)
			if err != nil {
				return AllowRule{}, err
			}
			rule.Ports = append(rule.Ports, port)
		}
	}
	return rule, nil
}

// parseAllowURL handles the URL form of ParseAllowEntry.
func parseAllowURL(entry string) (AllowRule, error) {
	u, err := url.Parse(entry)
	if err != nil {
		return AllowRule{}, err
	}
	var port int
	switch strings.ToLower(u.Scheme) {
	case "https":
		port = 443
	case "http":
		port = 80
	default:
		return AllowRule{}, fmt.Errorf("unsupported URL scheme %q: give the host and port instead (host:port)", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return AllowRule{}, errors.New("URL has no host")
	}
	if u.User != nil {
		return AllowRule{}, errors.New("URL contains credentials: an allow entry only needs the host")
	}
	if p := u.Port(); p != "" {
		if port, err = parsePort(p); err != nil {
			return AllowRule{}, err
		}
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		hostPort := net.JoinHostPort(host, strconv.Itoa(port))
		return AllowRule{}, fmt.Errorf("egress is filtered by host and port, not by URL path, so an allow entry can't be limited to this URL; to allow all of %s, use %q", host, hostPort)
	}
	if err := checkDomain(host); err != nil {
		return AllowRule{}, err
	}
	return AllowRule{Domain: host, Ports: []int{port}}, nil
}

func parsePort(s string) (int, error) {
	s = strings.TrimSpace(s)
	port, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("port %q is not a number", s)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("port %d is out of range (1-65535)", port)
	}
	return port, nil
}

// checkDomain reports whether d can be an AllowRule's domain: a hostname,
// a "*." wildcard over one, or an IP address (an IPv6 one without
// brackets). Anything else would never match a connection on either
// backend — Incus couldn't resolve it, and Lima's proxy would never see a
// request for it — so it's an error rather than a silently dead rule.
func checkDomain(d string) error {
	switch {
	case d == "":
		return errors.New("domain must not be empty")
	case strings.ContainsAny(d, " \t\r\n"):
		return fmt.Errorf("domain %q contains whitespace", d)
	}
	if _, _, err := net.ParseCIDR(d); err == nil {
		return fmt.Errorf("%q is an address range: allow rules take hostnames or single IP addresses", d)
	}
	if strings.ContainsAny(d, "/?#") {
		return fmt.Errorf("%q is not a hostname: egress is filtered by host and port, not by URL path", d)
	}
	name, wildcard := strings.CutPrefix(d, "*.")
	if net.ParseIP(name) != nil {
		if wildcard {
			return fmt.Errorf("%q: a wildcard can't apply to an IP address", d)
		}
		return nil
	}
	if strings.Contains(name, ":") {
		return fmt.Errorf("domain %q includes a port: list ports separately", d)
	}
	// A fully-qualified name may end in a dot; both backends accept that.
	name = strings.TrimSuffix(name, ".")
	labels := strings.Split(name, ".")
	if last := labels[len(labels)-1]; last != "" && strings.Trim(last, "0123456789") == "" {
		// No top-level domain is all digits, so this was meant as an IP
		// address (e.g. 10.0.0.300) — one that would never match.
		return fmt.Errorf("%q is not a valid IP address (and a hostname can't end in a number)", d)
	}
	if !validHostname(name) {
		return fmt.Errorf("%q is not a valid hostname, \"*.\" wildcard, or IP address", d)
	}
	return nil
}

// validHostname checks DNS hostname syntax: dot-separated labels of
// letters, digits, hyphens (not at either end) and underscores.
func validHostname(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return false
			}
		}
	}
	return true
}

// ReadAllowFile reads an allow file: one ParseAllowEntry entry per line.
// Blank lines are ignored, and "#" starts a comment that runs to the end
// of its line. Every invalid line is reported, by line number, rather than
// just the first.
func ReadAllowFile(path string) ([]AllowRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading allow file: %w", err)
	}
	var (
		rules []AllowRule
		errs  []error
	)
	// Some Windows editors start a UTF-8 file with a byte-order mark.
	text := strings.TrimPrefix(string(data), "\ufeff")
	for i, line := range strings.Split(text, "\n") {
		line, _, _ = strings.Cut(line, "#")
		if strings.TrimSpace(line) == "" {
			continue
		}
		rule, err := ParseAllowEntry(line)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s:%d: %w", path, i+1, err))
			continue
		}
		rules = append(rules, rule)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return rules, nil
}

// CheckLANLiterals checks allow rules against deny-LAN before a sandbox is
// created. Under deny-LAN the backends honour exactly one kind of LAN
// entry, an IP literal in a private range (RFC 1918, RFC 4193) with its
// ports listed; a private literal without ports, and any link-local
// literal, would be silently refused at run time, so they're errors here
// instead (loopback literals are CheckLoopbackLiterals' business). It
// returns the accepted private literals as host:ports entries, once each,
// for the caller to announce. Without deny-LAN it returns nothing.
func CheckLANLiterals(rules []AllowRule, denyLAN bool) ([]string, error) {
	if !denyLAN {
		return nil, nil
	}
	var entries []string
	seen := make(map[string]bool)
	for _, r := range rules {
		ip := net.ParseIP(strings.Trim(r.Domain, "[]"))
		if ip == nil {
			continue
		}
		switch {
		case ip.IsLinkLocalUnicast():
			return nil, fmt.Errorf("allow entry %q is a link-local address: deny-LAN refuses link-local addresses (where cloud metadata services live) even when listed", r.Domain)
		case !ip.IsPrivate():
			continue
		case len(r.Ports) == 0:
			example := r.Domain
			if strings.Contains(example, ":") {
				example = "[" + example + "]"
			}
			return nil, fmt.Errorf("allow entry %q is a LAN address: list the port(s) you need (e.g. %s:8080); deny-LAN permits a LAN host only on listed ports", r.Domain, example)
		}
		entries = appendEntry(entries, seen, r)
	}
	return entries, nil
}

// CheckLoopbackLiterals checks allow rules written as loopback literals,
// under any deny-LAN setting. The Lima backend's proxy, which runs on the
// host, honours exactly one kind: 127.0.0.1 or ::1 with its ports listed,
// which reaches that one service on the host. Without ports, or as any
// other loopback address, the entry would be silently refused at run
// time, so it's an error here instead. It returns the accepted entries as
// host:ports, once each, for the caller to announce.
func CheckLoopbackLiterals(rules []AllowRule) ([]string, error) {
	var entries []string
	seen := make(map[string]bool)
	for _, r := range rules {
		ip := net.ParseIP(strings.Trim(r.Domain, "[]"))
		switch {
		case ip == nil || !ip.IsLoopback():
			continue
		case !ip.Equal(net.IPv4(127, 0, 0, 1)) && !ip.Equal(net.IPv6loopback):
			return nil, fmt.Errorf("allow entry %q is a loopback address other than 127.0.0.1 or ::1: name the host's service as 127.0.0.1:<port> or [::1]:<port>", r.Domain)
		case len(r.Ports) == 0:
			example := "127.0.0.1"
			if ip.To4() == nil {
				example = "[::1]"
			}
			return nil, fmt.Errorf("allow entry %q is a loopback address: list the port(s) of the one host service you need (e.g. %s:8081); a loopback entry is permitted only on listed ports", r.Domain, example)
		}
		entries = appendEntry(entries, seen, r)
	}
	return entries, nil
}

// appendEntry appends r to entries as host:ports unless seen already has it.
func appendEntry(entries []string, seen map[string]bool, r AllowRule) []string {
	ports := make([]string, len(r.Ports))
	for i, p := range r.Ports {
		ports[i] = strconv.Itoa(p)
	}
	entry := net.JoinHostPort(strings.Trim(r.Domain, "[]"), strings.Join(ports, ","))
	if seen[entry] {
		return entries
	}
	seen[entry] = true
	return append(entries, entry)
}

// ResolvePath expands a leading "~" in p to the user's home directory, and
// joins a relative result onto baseDir — the directory of the file p was
// written in — so a path in a profile or spec means the same thing
// wherever agentctl is run from. With baseDir empty, a relative path stays
// relative to the working directory, as on the command line.
func ResolvePath(p, baseDir string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~"+string(filepath.Separator)) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expanding %q: %w", p, err)
		}
		p = filepath.Join(home, p[1:])
	}
	if !filepath.IsAbs(p) && baseDir != "" {
		p = filepath.Join(baseDir, p)
	}
	return p, nil
}

// ExpandAllowFile reads n.AllowFile, resolved against baseDir (see
// ResolvePath), appends its rules to n.Allow, and clears n.AllowFile: from
// then on the file's entries are ordinary allow rules, so it's read exactly
// once and no later layer (Merge, the providers) needs to know about it.
func (n *NetworkPolicy) ExpandAllowFile(baseDir string) error {
	if n.AllowFile == "" {
		return nil
	}
	path, err := ResolvePath(n.AllowFile, baseDir)
	if err != nil {
		return fmt.Errorf("network.allowFile: %w", err)
	}
	rules, err := ReadAllowFile(path)
	if err != nil {
		return fmt.Errorf("network.allowFile: %w", err)
	}
	n.Allow = append(n.Allow, rules...)
	n.AllowFile = ""
	return nil
}
