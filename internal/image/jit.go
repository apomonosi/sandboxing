package image

import (
	"bytes"
	"fmt"
	"text/template"
)

// BuildSpec is the minimal input to RenderCloudInit: a base image plus a
// list of packages to install. Intentionally small — see the package doc
// comment for what's deliberately not built yet.
type BuildSpec struct {
	BaseImage string
	Packages  []string
}

const cloudInitTemplate = `#cloud-config
package_update: true
packages:
{{- range .Packages }}
  - {{ . }}
{{- end }}
`

// RenderCloudInit renders a minimal cloud-init user-data document from
// spec, for a provider's ImageBuild to hand to its native build path.
func RenderCloudInit(spec BuildSpec) (string, error) {
	tmpl, err := template.New("cloud-init").Parse(cloudInitTemplate)
	if err != nil {
		return "", fmt.Errorf("parsing cloud-init template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, spec); err != nil {
		return "", fmt.Errorf("rendering cloud-init: %w", err)
	}
	return buf.String(), nil
}
