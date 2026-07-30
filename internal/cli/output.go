package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/apomonosi/sandboxing/internal/provider"
)

func newTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
}

// RenderInstances writes a list of instances as a table or, if
// jsonOutput, as an indented JSON array.
func RenderInstances(w io.Writer, instances []provider.Instance, jsonOutput bool) error {
	if jsonOutput {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(instances)
	}
	tw := newTabWriter(w)
	fmt.Fprintln(tw, "NAME\tSTATUS\tIMAGE\tIPS")
	for _, inst := range instances {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", inst.Name, inst.Status, inst.Image, strings.Join(inst.IPs, ","))
	}
	return tw.Flush()
}

// RenderInstance writes a single instance's detail as a table or JSON.
func RenderInstance(w io.Writer, inst *provider.Instance, jsonOutput bool) error {
	if jsonOutput {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(inst)
	}
	tw := newTabWriter(w)
	fmt.Fprintf(tw, "Name:\t%s\n", inst.Name)
	fmt.Fprintf(tw, "Provider:\t%s\n", inst.Provider)
	fmt.Fprintf(tw, "Status:\t%s\n", inst.Status)
	fmt.Fprintf(tw, "Image:\t%s\n", inst.Image)
	fmt.Fprintf(tw, "Profiles:\t%s\n", strings.Join(inst.Profiles, ","))
	fmt.Fprintf(tw, "IPs:\t%s\n", strings.Join(inst.IPs, ","))
	if !inst.CreatedAt.IsZero() {
		fmt.Fprintf(tw, "Created:\t%s\n", inst.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	}
	return tw.Flush()
}

// capabilityJSON is the --output=json shape for a capability-gate outcome.
type capabilityJSON struct {
	Status      string `json:"status"`
	Feature     string `json:"feature"`
	Provider    string `json:"provider"`
	Message     string `json:"message"`
	Plan        string `json:"plan,omitempty"`
	TrackingURL string `json:"tracking_url,omitempty"`
}

// RenderCapabilityJSON emits a capability-gate outcome as JSON, for
// scripts/CI to consume instead of parsing the human-readable message.
func RenderCapabilityJSON(w io.Writer, providerName string, cap provider.Capability) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(capabilityJSON{
		Status:      cap.Status.String(),
		Feature:     string(cap.Feature),
		Provider:    providerName,
		Message:     cap.Message,
		Plan:        cap.Plan,
		TrackingURL: cap.TrackingURL,
	})
}
