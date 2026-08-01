package incus

import "embed"

//go:embed embedded/bootstrap-user.sh
var embeddedFS embed.FS

func mustReadEmbedded(path string) string {
	data, err := embeddedFS.ReadFile(path)
	if err != nil {
		// Embedded assets are compiled into the binary; a missing file
		// here means the build itself is broken, not a runtime
		// condition callers can recover from — same reasoning as
		// internal/profile's mustLoadBuiltins.
		panic("incus: missing embedded asset " + path + ": " + err.Error())
	}
	return string(data)
}

var bootstrapUserScript = mustReadEmbedded("embedded/bootstrap-user.sh")
