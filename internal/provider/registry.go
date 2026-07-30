package provider

import "fmt"

// Factory builds a Provider instance. Kept separate from the Provider type
// itself so registration doesn't require constructing a provider (and its
// side effects, like checking for an installed binary) until it's actually
// selected.
type Factory func() (Provider, error)

// Registry maps a provider name (as used in agentctl's config and
// --provider flag) to a Factory. internal/cli holds one Registry, built
// from the real factories in production and from a fake in tests, so the
// command tree never hardcodes which providers exist.
type Registry struct {
	factories map[string]Factory
}

// NewRegistry builds an empty Registry.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

// Register adds a Factory under name. Registering the same name twice
// overwrites the previous entry, which is convenient for tests that want
// to swap in a fake provider under a real provider's name.
func (r *Registry) Register(name string, f Factory) {
	r.factories[name] = f
}

// Get constructs the Provider registered under name.
func (r *Registry) Get(name string) (Provider, error) {
	f, ok := r.factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q (known: %v)", name, r.Names())
	}
	return f()
}

// Names lists every registered provider name.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.factories))
	for n := range r.factories {
		names = append(names, n)
	}
	return names
}
