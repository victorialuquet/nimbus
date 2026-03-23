package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// Registry holds all successfully loaded and validated providers.
// It is safe for concurrent reads after construction.
type Registry struct {
	providers map[string]Provider
	hooks     hooks
}

type hooks struct {
	onLoad  func(name string)
	onError func(name string, err error)
}

// contextKey is the unexported key for storing a *Registry in a context.
type contextKey struct{}

// Option configures the behaviour of [Setup].
type Option func(*setupOptions)

type setupOptions struct {
	extra   []Provider
	pingAll bool
	onLoad  func(name string)
	onError func(name string, err error)
}

// WithProviders registers additional (custom) providers that can be declared
// in the PROVIDERS env variable alongside built-ins.
func WithProviders(ps ...Provider) Option {
	return func(o *setupOptions) { o.extra = append(o.extra, ps...) }
}

// WithPing enables a connectivity check ([Observable.Ping]) for every loaded
// provider that implements [Observable]. Errors cause Setup to fail.
func WithPing() Option {
	return func(o *setupOptions) { o.pingAll = true }
}

// WithObserver attaches load/error hooks for observability (logging, metrics).
//
//	provider.WithObserver(
//	    func(name string)           { slog.Info("provider loaded", "name", name) },
//	    func(name string, err error) { slog.Error("provider failed", "name", name, "err", err) },
//	)
func WithObserver(onLoad func(string), onError func(string, error)) Option {
	return func(o *setupOptions) {
		o.onLoad = onLoad
		o.onError = onError
	}
}

// Setup reads the PROVIDERS environment variable (comma-separated list of
// provider names), resolves each from the built-in or custom provider list,
// loads them in dependency order, validates them, and returns a ready Registry.
func Setup(ctx context.Context, opts ...Option) (*Registry, error) {
	o := &setupOptions{}
	for _, opt := range opts {
		opt(o)
	}

	catalog := buildCatalog(o.extra)

	names := parseProviderNames(os.Getenv("PROVIDERS"))
	if len(names) == 0 {
		return &Registry{providers: map[string]Provider{}}, nil
	}

	ordered, err := resolveDependencyOrder(names, catalog)
	if err != nil {
		return nil, err
	}

	reg := &Registry{
		providers: make(map[string]Provider, len(ordered)),
		hooks: hooks{
			onLoad:  o.onLoad,
			onError: o.onError,
		},
	}

	for _, name := range ordered {
		p, ok := catalog[name]
		if !ok {
			err := fmt.Errorf("provider: unknown provider %q — register it via WithProviders", name)
			reg.emitError(name, err)
			return nil, err
		}

		if err := p.Load(ctx); err != nil {
			wrapped := fmt.Errorf("provider: %s: load: %w", name, err)
			reg.emitError(name, wrapped)
			return nil, wrapped
		}

		if err := p.Validate(); err != nil {
			wrapped := fmt.Errorf("provider: %s: validate: %w", name, err)
			reg.emitError(name, wrapped)
			return nil, wrapped
		}

		if o.pingAll {
			if obs, ok := p.(Observable); ok {
				if err := obs.Ping(ctx); err != nil {
					wrapped := fmt.Errorf("provider: %s: ping: %w", name, err)
					reg.emitError(name, wrapped)
					return nil, wrapped
				}
			}
		}

		reg.providers[name] = p
		reg.emitLoad(name)
	}

	return reg, nil
}

// Inject stores reg in ctx and returns the new context.
func Inject(ctx context.Context, reg *Registry) context.Context {
	return context.WithValue(ctx, contextKey{}, reg)
}

// RegistryFromContext retrieves the *Registry from ctx.
// Returns nil if not present.
func RegistryFromContext(ctx context.Context) *Registry {
	r, _ := ctx.Value(contextKey{}).(*Registry)
	return r
}

// Retrieve returns the config of type T from the first provider in the registry
// whose [Provider.Config] returns a value assignable to *T.
func Retrieve[T any](ctx context.Context) (T, error) {
	reg := RegistryFromContext(ctx)
	if reg == nil {
		var zero T
		return zero, fmt.Errorf("%w: registry not in context", ErrNotFound)
	}
	for _, p := range reg.providers {
		if cfg, ok := p.Config().(T); ok {
			return cfg, nil
		}
	}
	var zero T
	return zero, fmt.Errorf("%w: no provider exposes config type %T", ErrNotFound, zero)
}

// RetrieveByName returns the config of type T from the named provider.
func RetrieveByName[T any](ctx context.Context, name string) (T, error) {
	reg := RegistryFromContext(ctx)
	if reg == nil {
		var zero T
		return zero, fmt.Errorf("%w: registry not in context", ErrNotFound)
	}
	p, ok := reg.providers[name]
	if !ok {
		var zero T
		return zero, fmt.Errorf("%w: provider %q not loaded", ErrNotFound, name)
	}
	cfg, ok := p.Config().(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("%w: provider %q config is not %T", ErrNotFound, name, zero)
	}
	return cfg, nil
}

// All returns a snapshot of all loaded providers, keyed by name.
func (r *Registry) All() map[string]Provider {
	out := make(map[string]Provider, len(r.providers))
	for k, v := range r.providers {
		out[k] = v
	}
	return out
}

// --- internal helpers ---

func (r *Registry) emitLoad(name string) {
	if r.hooks.onLoad != nil {
		r.hooks.onLoad(name)
	}
}

func (r *Registry) emitError(name string, err error) {
	if r.hooks.onError != nil {
		r.hooks.onError(name, err)
	}
}

func parseProviderNames(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if n := strings.TrimSpace(p); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// resolveDependencyOrder performs a simple topological sort on the provider
// names, respecting [Dependent.DependsOn] declarations.
func resolveDependencyOrder(names []string, catalog map[string]Provider) ([]string, error) {
	visited := make(map[string]bool, len(names))
	order := make([]string, 0, len(names))

	var visit func(name string, chain []string) error
	visit = func(name string, chain []string) error {
		if visited[name] {
			return nil
		}
		for _, c := range chain {
			if c == name {
				return fmt.Errorf("provider: dependency cycle detected: %s", strings.Join(append(chain, name), " → "))
			}
		}
		if p, ok := catalog[name]; ok {
			if dep, ok := p.(Dependent); ok {
				for _, d := range dep.DependsOn() {
					if err := visit(d, append(chain, name)); err != nil {
						return err
					}
				}
			}
		}
		visited[name] = true
		order = append(order, name)
		return nil
	}

	for _, n := range names {
		if err := visit(n, nil); err != nil {
			return nil, err
		}
	}
	return order, nil
}

// buildCatalog merges built-in providers with any custom ones supplied via
// [WithProviders]. Custom providers with the same name override built-ins.
func buildCatalog(custom []Provider) map[string]Provider {
	catalog := builtinCatalog()
	for _, p := range custom {
		catalog[p.Name()] = p
	}
	return catalog
}
