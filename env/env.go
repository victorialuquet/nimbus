// Package env provides typed, context-injected environment variable
// configuration for Go applications.
//
// It wraps the internal envparse engine and the go-playground/validator
// library so that a single call to [Setup] both parses and validates.
//
// # Struct tags
//
//	type Config struct {
//	    Port    int    `env:"PORT,default=8080"    validate:"min=1,max=65535"`
//	    DBUrl   string `env:"DATABASE_URL,required" validate:"url"`
//	    Debug   bool   `env:"DEBUG,default=false"`
//	}
package env

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
	"github.com/victorialuquet/nimbus/internal/envparse"
)

// ErrNotFound is returned by [From] when the requested config type has not
// been injected into the context.
var ErrNotFound = errors.New("env: config not found in context")

// contextKey is an unexported type used as a context key to avoid collisions.
type contextKey[T any] struct{}

// Option configures the behaviour of [Setup].
type Option func(*options)

type options struct {
	dotenvFiles []string
	lookuper    envparse.Lookuper
}

// WithDotenv instructs [Setup] to load the given .env files before parsing.
// Files are loaded in order; later files do not override earlier ones.
// Defaults to ".env" when no files are specified.
func WithDotenv(files ...string) Option {
	return func(o *options) {
		if len(files) == 0 {
			files = []string{".env"}
		}
		o.dotenvFiles = files
	}
}

// WithLookuper replaces the default [os.LookupEnv] resolver. Useful in tests
// to inject a fake environment without mutating os.Environ.
func WithLookuper(l envparse.Lookuper) Option {
	return func(o *options) { o.lookuper = l }
}

// Setup parses environment variables into a new T, validates the result using
// struct tags, and returns a pointer to the populated struct.
//
// Parsing uses the `env` struct tag; validation uses the `validate` struct tag
// (go-playground/validator rules).
func Setup[T any](ctx context.Context, opts ...Option) (*T, error) {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	// Optional .env loading — errors are intentionally ignored when the file
	// does not exist (consistent with how dotenv works in other ecosystems).
	if len(o.dotenvFiles) > 0 {
		_ = godotenv.Overload(o.dotenvFiles...)
	}

	var cfg T
	if err := envparse.Process(&cfg, o.lookuper); err != nil {
		return nil, fmt.Errorf("env: parse: %w", err)
	}

	validate := validator.New()
	if err := validate.StructCtx(ctx, &cfg); err != nil {
		return nil, fmt.Errorf("env: validate: %w", err)
	}

	return &cfg, nil
}

// Inject stores cfg into ctx and returns the new context.
// The key is the concrete type of T, so different config types do not collide.
func Inject[T any](ctx context.Context, cfg *T) context.Context {
	return context.WithValue(ctx, contextKey[T]{}, cfg)
}

// From retrieves the config of type T previously stored by [Inject].
// Returns [ErrNotFound] if absent.
func From[T any](ctx context.Context) (T, error) {
	v, ok := ctx.Value(contextKey[T]{}).(*T)
	if !ok || v == nil {
		var zero T
		return zero, fmt.Errorf("%w: %T", ErrNotFound, zero)
	}
	return *v, nil
}

// MustFrom is like [From] but panics on missing config. Use during
// application startup where absence indicates a programming error.
func MustFrom[T any](ctx context.Context) T {
	cfg, err := From[T](ctx)
	if err != nil {
		panic(err)
	}
	return cfg
}
