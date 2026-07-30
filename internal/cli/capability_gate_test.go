package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/apomonosi/sandboxing/internal/provider"
)

func TestCapabilityGate_Supported(t *testing.T) {
	var buf bytes.Buffer
	result, code := CapabilityGate(&buf, "fake", provider.Capability{Feature: provider.FeatureCreate, Status: provider.Supported}, false)
	if result != GateProceed {
		t.Errorf("want GateProceed, got %v", result)
	}
	if code != 0 {
		t.Errorf("want exit code 0, got %d", code)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output for Supported, got %q", buf.String())
	}
}

func TestCapabilityGate_UnderDevelopment(t *testing.T) {
	var buf bytes.Buffer
	cap := provider.Capability{
		Feature:     provider.FeatureCreate,
		Status:      provider.UnderDevelopment,
		Message:     "not wired up yet",
		TrackingURL: "https://example.com/issue/1",
	}
	result, code := CapabilityGate(&buf, "lima", cap, false)
	if result != GateBlocked {
		t.Errorf("want GateBlocked, got %v", result)
	}
	if code != 2 {
		t.Errorf("want exit code 2, got %d", code)
	}
	out := buf.String()
	for _, want := range []string{"under development", "lima", "not wired up yet", "https://example.com/issue/1"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q missing %q", out, want)
		}
	}
}

func TestCapabilityGate_NotAvailable(t *testing.T) {
	var buf bytes.Buffer
	cap := provider.Capability{Feature: provider.FeatureSnapshotCreate, Status: provider.NotAvailable, Message: "unstable upstream"}
	result, code := CapabilityGate(&buf, "lima", cap, false)
	if result != GateBlocked || code != 2 {
		t.Errorf("want GateBlocked/2, got %v/%d", result, code)
	}
	if !strings.Contains(buf.String(), "not available") {
		t.Errorf("output %q missing 'not available'", buf.String())
	}
}

func TestCapabilityGate_ManualWorkaround_NotForced(t *testing.T) {
	var buf bytes.Buffer
	cap := provider.Capability{
		Feature: provider.FeatureNetworkACL,
		Status:  provider.ManualWorkaround,
		Message: "no native ACL",
		Plan:    "do this pf rule",
	}
	result, code := CapabilityGate(&buf, "lima", cap, false)
	if result != GateBlocked {
		t.Errorf("want GateBlocked, got %v", result)
	}
	if code != 3 {
		t.Errorf("want exit code 3, got %d", code)
	}
	out := buf.String()
	for _, want := range []string{"no native ACL", "do this pf rule", "--force-partial"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q missing %q", out, want)
		}
	}
}

func TestCapabilityGate_ManualWorkaround_Forced(t *testing.T) {
	var buf bytes.Buffer
	cap := provider.Capability{
		Feature: provider.FeatureNetworkACL,
		Status:  provider.ManualWorkaround,
		Message: "no native ACL",
		Plan:    "do this pf rule",
	}
	result, code := CapabilityGate(&buf, "lima", cap, true)
	if result != GateProceed {
		t.Errorf("want GateProceed with force-partial, got %v", result)
	}
	if code != 0 {
		t.Errorf("want exit code 0, got %d", code)
	}
	if !strings.Contains(buf.String(), "Continuing without this protection") {
		t.Errorf("expected warning banner, got %q", buf.String())
	}
}

func TestGateOrBlock_ReturnsExitError(t *testing.T) {
	var buf bytes.Buffer
	err := gateOrBlock(&buf, "lima", provider.Capability{Feature: provider.FeatureCreate, Status: provider.UnderDevelopment}, false)
	if err == nil {
		t.Fatal("expected an error")
	}
	exitErr, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError, got %T", err)
	}
	if exitErr.Code != 2 {
		t.Errorf("want exit code 2, got %d", exitErr.Code)
	}
}

func TestGateOrBlock_Supported_ReturnsNil(t *testing.T) {
	var buf bytes.Buffer
	err := gateOrBlock(&buf, "incus", provider.Capability{Feature: provider.FeatureCreate, Status: provider.Supported}, false)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}
