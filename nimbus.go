// Package nimbus is a multi-cloud configuration manager for Go.
// It provides typed, context-injected configuration for both application
// environment variables and cloud provider credentials.
//
// # Quick start
//
//	ctx := context.Background()
//
// //	1. Load app env config
//
//	ctx, err := nimbus.SetupEnv[AppConfig](ctx)
//
// //	2. Load cloud providers declared in PROVIDERS=aws,gcp
//
//	ctx, err = nimbus.SetupProviders(ctx)
//
// //	3. Retrieve anywhere downstream
//
//	awsCfg, err := nimbus.Retrieve[*aws.Config](ctx)
package nimbus

import (
	"context"
	"fmt"

	"github.com/victorialuquet/nimbus/env"
	"github.com/victorialuquet/nimbus/provider"
)

// SetupEnv parses environment variables into T, validates the result, and
// injects the config into the returned context.
// T must be a struct with `env` and `validate` tags.
func SetupEnv[T any](ctx context.Context, opts ...env.Option) (context.Context, error) {
	cfg, err := env.Setup[T](ctx, opts...)
	if err != nil {
		return ctx, fmt.Errorf("nimbus: env setup: %w", err)
	}
	return env.Inject[T](ctx, cfg), nil
}

// SetupProviders reads the PROVIDERS environment variable, initialises each
// declared provider, validates their config, and injects the registry into the
// returned context.
//
// Built-in provider names: "aws", "gcp", "azure".
// Pass custom providers via [provider.WithProviders].
func SetupProviders(ctx context.Context, opts ...provider.Option) (context.Context, error) {
	reg, err := provider.Setup(ctx, opts...)
	if err != nil {
		return ctx, fmt.Errorf("nimbus: provider setup: %w", err)
	}
	return provider.Inject(ctx, reg), nil
}

// Retrieve returns the config of type T from the provider registry stored in
// ctx. Use [RetrieveByName] when multiple instances of the same provider type
// are registered (e.g. aws-prod and aws-staging).
//
// Returns [provider.ErrNotFound] if no matching provider is loaded.
func Retrieve[T any](ctx context.Context) (T, error) {
	return provider.Retrieve[T](ctx)
}

// RetrieveByName returns the config of type T from the provider with the given
// registered name (e.g. "aws-eu").
func RetrieveByName[T any](ctx context.Context, name string) (T, error) {
	return provider.RetrieveByName[T](ctx, name)
}

// MustRetrieve is like [Retrieve] but panics if the provider is not found.
// Intended for use during application initialisation where absence is a
// programming error.
func MustRetrieve[T any](ctx context.Context) T {
	cfg, err := Retrieve[T](ctx)
	if err != nil {
		panic(err)
	}
	return cfg
}

// EnvFrom returns the typed env config T previously injected by [SetupEnv].
// Returns [env.ErrNotFound] if not present.
func EnvFrom[T any](ctx context.Context) (T, error) {
	return env.From[T](ctx)
}

// MustEnvFrom is like [EnvFrom] but panics if the config is not in context.
func MustEnvFrom[T any](ctx context.Context) T {
	cfg, err := env.From[T](ctx)
	if err != nil {
		panic(err)
	}
	return cfg
}
