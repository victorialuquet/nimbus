# CLAUDE.md — cloudcfg

Concise guide for AI-assisted development on this project.

## What this project is

`nimbus` is a **typed, context-injected, multi-cloud configuration manager** for Go.
Two concerns, cleanly separated:

1. **Env config** (`env/`) — parse os.Environ into a user-defined struct, validate with `go-playground/validator`.
2. **Provider config** (`provider/`, `providers/`) — load cloud credentials declared in `PROVIDERS=aws,gcp`, inject into context, retrieve with generics.

## Package layout

```
nimbus.go          ← public surface: SetupEnv, SetupProviders, Retrieve, MustRetrieve
env/                 ← Setup[T], Inject, From, MustFrom
provider/            ← Provider interface, Registry, Retrieve[T], RetrieveByName[T]
providers/aws|gcp|azure ← built-in provider implementations
internal/envparse/   ← zero-dep env→struct parser (do not import from outside internal/)
examples/            ← runnable usage examples
```

## Go conventions to follow

- **Errors**: wrap with `%w`, return early, never `log.Fatal` inside library code.
- **Context**: always first argument, named `ctx`. Never store in structs.
- **Interfaces**: define at the call site (consumer), not the implementation package.
- **Generics**: use only where the type parameter provides real value. Avoid `any` returns when a concrete type is possible.
- **Exported API**: every exported symbol needs a godoc comment. Start with the symbol name.
- **Unexported helpers**: keep in the same file unless reused across files in the same package.
- **No globals**: no `sync.Once`-initialised package-level state. Pass `*Registry` and configs explicitly via context.

## Key design constraints

- `internal/envparse` must have **zero external dependencies** — no new imports.
- The two allowed external deps are `go-playground/validator` (validation tags) and `joho/godotenv` (`.env` files). Do not add more without discussion.
- `provider.Provider` is the extension point for custom providers — implement the interface, pass via `WithProviders`.
- Context keys are **unexported typed structs** (`contextKey[T]{}`), never strings.
- `Retrieve[T]` does a linear scan over loaded providers — keep provider count small; this is not a hot path.

## Adding a new built-in provider

1. Create `providers/<name>/<name>.go` with a `Config` struct and a `Provider` struct.
2. Implement `Name() string`, `Load(ctx) error`, `Validate() error`, `Config() any`.
3. Register in `provider/catalog.go → builtinCatalog()`.
4. Add env var documentation in the package doc comment.
5. Add an example or extend `examples/multicloud/`.

## Adding optional provider capabilities

- Credential rotation → implement `Refreshable` (`Refresh(ctx) error`).
- Health check → implement `Observable` (`Ping(ctx) error`).
- Load ordering → implement `Dependent` (`DependsOn() []string`).

## Testing approach

- Inject a fake env via `env.WithLookuper(func(k string) (string, bool) { ... })` — never mutate `os.Environ` in tests.
- Use `provider.WithProviders` to inject stub providers in registry tests.
- Provider `Validate()` logic should have table-driven tests covering all auth-method combinations.
