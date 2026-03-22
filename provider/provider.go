// Package provider defines the Provider interface and the Registry that manages
// the lifecycle of cloud provider configurations.
//
// # Built-in providers
//
// The following names are recognised in the PROVIDERS environment variable:
// "aws", "gcp", "azure".
//
// # Custom providers
//
// Implement [Provider] and pass instances via [WithProviders]:
//
//	type MyProvider struct{ cfg MyConfig }
//	func (p *MyProvider) Name() string                       { return "myprovider" }
//	func (p *MyProvider) Load(ctx context.Context) error     { ... }
//	func (p *MyProvider) Validate() error                    { ... }
//	func (p *MyProvider) Config() any                        { return &p.cfg }
//
//	ctx, err := cloudcfg.SetupProviders(ctx, provider.WithProviders(&MyProvider{}))
package provider

import (
	"context"
	"errors"
)

// ErrNotFound is returned when no provider matching the requested type or name
// is present in the registry.
var ErrNotFound = errors.New("provider: not found in registry")

// Provider is the interface every cloud provider must satisfy.
//
// Implementations should be safe for concurrent reads after [Load] returns.
type Provider interface {
	// Name returns the unique identifier for this provider instance.
	// Built-in examples: "aws", "gcp", "azure".
	// Multi-region instances should use qualified names: "aws-eu", "aws-us".
	Name() string

	// Load reads all required configuration (typically from environment
	// variables) and initialises the provider's internal state.
	// Called once during [Setup].
	Load(ctx context.Context) error

	// Validate performs semantic validation after [Load].
	// Return a descriptive error when required fields are missing or
	// mutually exclusive options are set inconsistently.
	Validate() error

	// Config returns the provider's typed configuration struct.
	// The return value is used by [Retrieve] for type-based lookup.
	Config() any
}

// Refreshable is an optional extension of [Provider] for providers whose
// credentials can rotate (e.g. short-lived tokens, OIDC).
type Refreshable interface {
	Provider

	// Refresh reloads and re-validates credentials without restarting the
	// application. Implementations must be safe for concurrent callers.
	Refresh(ctx context.Context) error
}

// Observable is an optional extension of [Provider] for providers that expose
// a health/connectivity check.
type Observable interface {
	Provider

	// Ping verifies that the provider's remote endpoint is reachable.
	// Called post-load when the registry's [WithPing] option is enabled.
	Ping(ctx context.Context) error
}

// Dependent is an optional extension of [Provider] for providers that require
// other providers to be loaded first.
type Dependent interface {
	Provider

	// DependsOn returns names of providers that must be loaded before this one.
	DependsOn() []string
}
