package provider_test

import (
	"testing"

	"github.com/apomonosi/sandboxing/internal/provider"
	"github.com/apomonosi/sandboxing/internal/provider/fake"
	"github.com/apomonosi/sandboxing/internal/provider/hyperv"
	"github.com/apomonosi/sandboxing/internal/provider/incus"
	"github.com/apomonosi/sandboxing/internal/provider/lima"
)

// TestCapabilityTableCompleteness guards against a new Feature constant
// being added without every provider (including the stubs) being updated
// to declare an entry for it. A missing entry silently degrades to
// NotAvailable via Table.Get, which would be a confusing way to discover
// a forgotten wiring step.
func TestCapabilityTableCompleteness(t *testing.T) {
	incusP, err := incus.New()
	if err != nil {
		t.Fatalf("incus.New: %v", err)
	}
	limaP, err := lima.New()
	if err != nil {
		t.Fatalf("lima.New: %v", err)
	}
	hypervP, err := hyperv.New()
	if err != nil {
		t.Fatalf("hyperv.New: %v", err)
	}

	providers := map[string]provider.Provider{
		"fake":   fake.New(),
		"incus":  incusP,
		"lima":   limaP,
		"hyperv": hypervP,
	}

	for name, p := range providers {
		table := p.Capabilities()
		for _, f := range provider.AllFeatures {
			cap, ok := table[f]
			if !ok {
				t.Errorf("provider %q: missing capability entry for feature %q", name, f)
				continue
			}
			if cap.Feature != f {
				t.Errorf("provider %q: capability entry for %q has Feature=%q (mismatch)", name, f, cap.Feature)
			}
		}
	}
}
