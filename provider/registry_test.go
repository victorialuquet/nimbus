package provider_test

import (
	"context"
	"errors"
	"testing"

	"github.com/victorialuquet/nimbus/provider"
)

// ── Stub provider ──────────────────────────────────────────────────────────

// stubProvider is a minimal Provider implementation used across all registry tests.
type stubProvider struct {
	name       string
	loadErr    error
	validateErr error
	cfg        any
	pingErr    error
	deps       []string
	loadCalled bool
}

func (p *stubProvider) Name() string                   { return p.name }
func (p *stubProvider) Load(_ context.Context) error   { p.loadCalled = true; return p.loadErr }
func (p *stubProvider) Validate() error                 { return p.validateErr }
func (p *stubProvider) Config() any                     { return p.cfg }

// stubObservable extends stubProvider with Ping support.
type stubObservable struct {
	stubProvider
}

func (p *stubObservable) Ping(_ context.Context) error { return p.pingErr }

// stubDependent extends stubProvider with DependsOn support.
type stubDependent struct {
	stubProvider
}

func (p *stubDependent) DependsOn() []string { return p.deps }

// ── helpers ────────────────────────────────────────────────────────────────

// setProviders sets the PROVIDERS env variable and restores it via t.Cleanup.
func setProviders(t *testing.T, val string) {
	t.Helper()
	t.Setenv("PROVIDERS", val)
}

// ── Empty PROVIDERS ────────────────────────────────────────────────────────

func TestSetup_empty_providers(t *testing.T) {
	setProviders(t, "")

	reg, err := provider.Setup(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reg.All()) != 0 {
		t.Errorf("expected empty registry, got %d providers", len(reg.All()))
	}
}

// ── Unknown provider ───────────────────────────────────────────────────────

func TestSetup_unknown_provider(t *testing.T) {
	setProviders(t, "nonexistent")

	_, err := provider.Setup(context.Background())
	if err == nil {
		t.Error("expected error for unknown provider, got nil")
	}
}

// ── Load error ─────────────────────────────────────────────────────────────

func TestSetup_load_error(t *testing.T) {
	stub := &stubProvider{
		name:    "fail",
		loadErr: errors.New("connection refused"),
	}
	setProviders(t, "fail")

	_, err := provider.Setup(context.Background(), provider.WithProviders(stub))
	if err == nil {
		t.Error("expected error when Load fails, got nil")
	}
}

// ── Validate error ─────────────────────────────────────────────────────────

func TestSetup_validate_error(t *testing.T) {
	stub := &stubProvider{
		name:        "bad",
		validateErr: errors.New("missing required field"),
	}
	setProviders(t, "bad")

	_, err := provider.Setup(context.Background(), provider.WithProviders(stub))
	if err == nil {
		t.Error("expected error when Validate fails, got nil")
	}
}

// ── Ping ───────────────────────────────────────────────────────────────────

func TestSetup_ping_success(t *testing.T) {
	stub := &stubObservable{
		stubProvider: stubProvider{name: "obs", cfg: "cfg"},
	}
	setProviders(t, "obs")

	reg, err := provider.Setup(context.Background(),
		provider.WithProviders(stub),
		provider.WithPing(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := reg.All()["obs"]; !ok {
		t.Error("expected provider 'obs' in registry")
	}
}

func TestSetup_ping_error(t *testing.T) {
	stub := &stubObservable{
		stubProvider: stubProvider{name: "obs", pingErr: errors.New("unreachable")},
	}
	setProviders(t, "obs")

	_, err := provider.Setup(context.Background(),
		provider.WithProviders(stub),
		provider.WithPing(),
	)
	if err == nil {
		t.Error("expected error when Ping fails, got nil")
	}
}

func TestSetup_ping_not_called_without_option(t *testing.T) {
	stub := &stubObservable{
		stubProvider: stubProvider{name: "obs", cfg: "cfg", pingErr: errors.New("should not be called")},
	}
	setProviders(t, "obs")

	// No WithPing() — even though pingErr is set, Setup should succeed.
	_, err := provider.Setup(context.Background(), provider.WithProviders(stub))
	if err != nil {
		t.Fatalf("unexpected error (Ping should not be called): %v", err)
	}
}

// ── Observer hooks ─────────────────────────────────────────────────────────

func TestSetup_observer_onLoad(t *testing.T) {
	stub := &stubProvider{name: "p1", cfg: "cfg"}
	setProviders(t, "p1")

	var loaded []string
	_, err := provider.Setup(context.Background(),
		provider.WithProviders(stub),
		provider.WithObserver(
			func(name string) { loaded = append(loaded, name) },
			nil,
		),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(loaded) != 1 || loaded[0] != "p1" {
		t.Errorf("onLoad called with %v, want [p1]", loaded)
	}
}

func TestSetup_observer_onError(t *testing.T) {
	stub := &stubProvider{name: "bad", loadErr: errors.New("boom")}
	setProviders(t, "bad")

	var errored []string
	provider.Setup(context.Background(), //nolint:errcheck
		provider.WithProviders(stub),
		provider.WithObserver(
			nil,
			func(name string, _ error) { errored = append(errored, name) },
		),
	)
	if len(errored) != 1 || errored[0] != "bad" {
		t.Errorf("onError called with %v, want [bad]", errored)
	}
}

// ── Dependency ordering ────────────────────────────────────────────────────

func TestSetup_dependency_ordering(t *testing.T) {
	var loadOrder []string

	// Override Load to record order.
	base2 := &recordingProvider{stubProvider: stubProvider{name: "base", cfg: "base-cfg"}, order: &loadOrder}
	child := &recordingDependent{
		stubDependent: stubDependent{
			stubProvider: stubProvider{name: "child", cfg: "child-cfg", deps: []string{"base"}},
		},
		order: &loadOrder,
	}

	setProviders(t, "child,base")

	_, err := provider.Setup(context.Background(),
		provider.WithProviders(base2, child),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// "base" must be loaded before "child".
	if len(loadOrder) != 2 {
		t.Fatalf("expected 2 load calls, got %d: %v", len(loadOrder), loadOrder)
	}
	if loadOrder[0] != "base" || loadOrder[1] != "child" {
		t.Errorf("load order = %v, want [base child]", loadOrder)
	}
}

// recordingProvider records its name into a shared slice on Load.
type recordingProvider struct {
	stubProvider
	order *[]string
}

func (p *recordingProvider) Load(_ context.Context) error {
	*p.order = append(*p.order, p.name)
	return nil
}

// recordingDependent combines recording with DependsOn.
type recordingDependent struct {
	stubDependent
	order *[]string
}

func (p *recordingDependent) Load(_ context.Context) error {
	*p.order = append(*p.order, p.name)
	return nil
}

func (p *recordingDependent) DependsOn() []string { return p.deps }

// ── Cycle detection ────────────────────────────────────────────────────────

func TestSetup_cycle_detection(t *testing.T) {
	a := &stubDependent{
		stubProvider: stubProvider{name: "a", deps: []string{"b"}},
	}
	b := &stubDependent{
		stubProvider: stubProvider{name: "b", deps: []string{"a"}},
	}
	setProviders(t, "a,b")

	_, err := provider.Setup(context.Background(), provider.WithProviders(a, b))
	if err == nil {
		t.Error("expected cycle detection error, got nil")
	}
}

// ── Retrieve ───────────────────────────────────────────────────────────────

type myConfig struct{ Region string }

func TestRetrieve_found(t *testing.T) {
	stub := &stubProvider{name: "p", cfg: myConfig{Region: "us-east-1"}}
	setProviders(t, "p")

	reg, err := provider.Setup(context.Background(), provider.WithProviders(stub))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := provider.Inject(context.Background(), reg)
	cfg, err := provider.Retrieve[myConfig](ctx)
	if err != nil {
		t.Fatalf("Retrieve returned error: %v", err)
	}
	if cfg.Region != "us-east-1" {
		t.Errorf("Region = %q, want us-east-1", cfg.Region)
	}
}

func TestRetrieve_not_found(t *testing.T) {
	stub := &stubProvider{name: "p", cfg: myConfig{Region: "us-east-1"}}
	setProviders(t, "p")

	reg, err := provider.Setup(context.Background(), provider.WithProviders(stub))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := provider.Inject(context.Background(), reg)

	type otherConfig struct{}
	_, err = provider.Retrieve[otherConfig](ctx)
	if err == nil {
		t.Error("expected ErrNotFound, got nil")
	}
	if !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("error = %v, want wrapping ErrNotFound", err)
	}
}

func TestRetrieve_no_registry_in_context(t *testing.T) {
	_, err := provider.Retrieve[myConfig](context.Background())
	if err == nil {
		t.Error("expected error when registry not in context, got nil")
	}
	if !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("error = %v, want wrapping ErrNotFound", err)
	}
}

// ── RetrieveByName ─────────────────────────────────────────────────────────

func TestRetrieveByName_found(t *testing.T) {
	stub := &stubProvider{name: "myp", cfg: myConfig{Region: "eu-west-1"}}
	setProviders(t, "myp")

	reg, err := provider.Setup(context.Background(), provider.WithProviders(stub))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := provider.Inject(context.Background(), reg)
	cfg, err := provider.RetrieveByName[myConfig](ctx, "myp")
	if err != nil {
		t.Fatalf("RetrieveByName returned error: %v", err)
	}
	if cfg.Region != "eu-west-1" {
		t.Errorf("Region = %q, want eu-west-1", cfg.Region)
	}
}

func TestRetrieveByName_unknown_name(t *testing.T) {
	stub := &stubProvider{name: "p", cfg: myConfig{}}
	setProviders(t, "p")

	reg, err := provider.Setup(context.Background(), provider.WithProviders(stub))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := provider.Inject(context.Background(), reg)
	_, err = provider.RetrieveByName[myConfig](ctx, "ghost")
	if err == nil {
		t.Error("expected ErrNotFound for unknown name, got nil")
	}
	if !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("error = %v, want wrapping ErrNotFound", err)
	}
}

func TestRetrieveByName_wrong_type(t *testing.T) {
	stub := &stubProvider{name: "p", cfg: myConfig{Region: "us"}}
	setProviders(t, "p")

	reg, err := provider.Setup(context.Background(), provider.WithProviders(stub))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := provider.Inject(context.Background(), reg)

	type differentConfig struct{}
	_, err = provider.RetrieveByName[differentConfig](ctx, "p")
	if err == nil {
		t.Error("expected ErrNotFound for type mismatch, got nil")
	}
}

func TestRetrieveByName_no_registry_in_context(t *testing.T) {
	_, err := provider.RetrieveByName[myConfig](context.Background(), "p")
	if err == nil {
		t.Error("expected error when registry not in context, got nil")
	}
	if !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("error = %v, want wrapping ErrNotFound", err)
	}
}

// ── All ────────────────────────────────────────────────────────────────────

func TestRegistry_All(t *testing.T) {
	p1 := &stubProvider{name: "p1", cfg: "c1"}
	p2 := &stubProvider{name: "p2", cfg: "c2"}
	setProviders(t, "p1,p2")

	reg, err := provider.Setup(context.Background(), provider.WithProviders(p1, p2))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	all := reg.All()
	if len(all) != 2 {
		t.Errorf("All() len = %d, want 2", len(all))
	}
	if _, ok := all["p1"]; !ok {
		t.Error("expected p1 in All()")
	}
	if _, ok := all["p2"]; !ok {
		t.Error("expected p2 in All()")
	}
}

func TestRegistry_All_returns_copy(t *testing.T) {
	stub := &stubProvider{name: "p", cfg: "c"}
	setProviders(t, "p")

	reg, err := provider.Setup(context.Background(), provider.WithProviders(stub))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	snap1 := reg.All()
	snap1["injected"] = &stubProvider{name: "injected"}

	snap2 := reg.All()
	if _, ok := snap2["injected"]; ok {
		t.Error("All() should return a copy — mutation leaked back into registry")
	}
}

// ── RegistryFromContext ────────────────────────────────────────────────────

func TestRegistryFromContext_absent(t *testing.T) {
	reg := provider.RegistryFromContext(context.Background())
	if reg != nil {
		t.Error("expected nil registry when not injected")
	}
}

func TestRegistryFromContext_present(t *testing.T) {
	setProviders(t, "")
	reg, err := provider.Setup(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := provider.Inject(context.Background(), reg)
	got := provider.RegistryFromContext(ctx)
	if got == nil {
		t.Error("expected non-nil registry after Inject")
	}
}

// ── Custom provider overrides built-in ─────────────────────────────────────

func TestSetup_custom_overrides_builtin(t *testing.T) {
	// "aws" is a built-in name; our custom stub should take precedence.
	custom := &stubProvider{name: "aws", cfg: "custom-aws-cfg"}
	setProviders(t, "aws")

	reg, err := provider.Setup(context.Background(), provider.WithProviders(custom))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := provider.Inject(context.Background(), reg)
	cfg, err := provider.Retrieve[string](ctx)
	if err != nil {
		t.Fatalf("Retrieve returned error: %v", err)
	}
	if cfg != "custom-aws-cfg" {
		t.Errorf("cfg = %q, want custom-aws-cfg", cfg)
	}
}
