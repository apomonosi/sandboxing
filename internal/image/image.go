// Package image handles image reference parsing and a minimal
// just-in-time build path for `agentctl image pull` / `agentctl image
// build`. A full pipeline (registry signing/verification, multi-arch
// builds, Lima-template/Hyper-V-VHDX build support) is out of scope for
// this milestone — see the project plan.
package image

import "strings"

// Ref is a parsed image reference.
type Ref struct {
	Raw   string
	Local bool // true if Raw looks like a local filesystem path
}

// ParseRef classifies raw as local (a filesystem path) or remote (a
// registry/simplestreams reference), without touching the filesystem.
func ParseRef(raw string) Ref {
	local := strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "~")
	return Ref{Raw: raw, Local: local}
}
