package azure

import (
	"fmt"
	"testing"

	"github.com/victorialuquet/nimbus/internal/envparse"
)

// fakeEnv builds a Lookuper from a static map.
func fakeEnv(kv map[string]string) envparse.Lookuper {
	return func(key string) (string, bool) {
		v, ok := kv[key]
		return v, ok
	}
}

// loadParams populates a Provider's params using an injected lookuper,
// without triggering credential construction (which may contact Azure).
func loadParams(p *Provider, kv map[string]string) error {
	return envparse.Process(&p.p, fakeEnv(kv))
}

// ── Name ─────────────────────────────────────────────────────────────────────

func TestProvider_Name_default(t *testing.T) {
	p := &Provider{}
	if got := p.Name(); got != "azure" {
		t.Errorf("Name() = %q, want %q", got, "azure")
	}
}

func TestProvider_Name_custom(t *testing.T) {
	p := NewProvider("azure-prod", "")
	if got := p.Name(); got != "azure-prod" {
		t.Errorf("Name() = %q, want %q", got, "azure-prod")
	}
}

// ── Validate ──────────────────────────────────────────────────────────────────

func TestProvider_Validate(t *testing.T) {
	tests := []struct {
		name    string
		kv      map[string]string
		wantErr bool
	}{
		{
			name: "subscription ID only — default chain",
			kv:   map[string]string{"AZURE_SUBSCRIPTION_ID": "sub-123"},
		},
		{
			name: "full service principal",
			kv: map[string]string{
				"AZURE_SUBSCRIPTION_ID": "sub-123",
				"AZURE_TENANT_ID":       "tenant-abc",
				"AZURE_CLIENT_ID":       "client-xyz",
				"AZURE_CLIENT_SECRET":   "secret",
			},
		},
		{
			name: "secret without client ID — should error",
			kv: map[string]string{
				"AZURE_SUBSCRIPTION_ID": "sub-123",
				"AZURE_CLIENT_SECRET":   "secret",
			},
			wantErr: true,
		},
		{
			name: "secret without tenant ID — should error",
			kv: map[string]string{
				"AZURE_SUBSCRIPTION_ID": "sub-123",
				"AZURE_CLIENT_ID":       "client-xyz",
				"AZURE_CLIENT_SECRET":   "secret",
			},
			wantErr: true,
		},
		{
			name: "client ID only — managed identity, no secret required",
			kv: map[string]string{
				"AZURE_SUBSCRIPTION_ID": "sub-123",
				"AZURE_CLIENT_ID":       "client-xyz",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Provider{}
			if err := loadParams(p, tt.kv); err != nil && !tt.wantErr {
				t.Fatalf("loadParams: unexpected error: %v", err)
			}
			err := p.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ── buildCredential ───────────────────────────────────────────────────────────

func TestProvider_buildCredential(t *testing.T) {
	tests := []struct {
		name     string
		kv       map[string]string
		wantType string
	}{
		{
			name: "service principal → ClientSecretCredential",
			kv: map[string]string{
				"AZURE_SUBSCRIPTION_ID": "sub-123",
				"AZURE_TENANT_ID":       "tenant-abc",
				"AZURE_CLIENT_ID":       "client-xyz",
				"AZURE_CLIENT_SECRET":   "secret",
			},
			wantType: "*azidentity.ClientSecretCredential",
		},
		{
			name: "client ID only → ManagedIdentityCredential",
			kv: map[string]string{
				"AZURE_SUBSCRIPTION_ID": "sub-123",
				"AZURE_CLIENT_ID":       "client-xyz",
			},
			wantType: "*azidentity.ManagedIdentityCredential",
		},
		{
			name: "no credentials → DefaultAzureCredential",
			kv: map[string]string{
				"AZURE_SUBSCRIPTION_ID": "sub-123",
			},
			wantType: "*azidentity.DefaultAzureCredential",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Provider{}
			if err := loadParams(p, tt.kv); err != nil {
				t.Fatalf("loadParams: %v", err)
			}
			cred, err := p.buildCredential()
			if err != nil {
				t.Fatalf("buildCredential() error = %v", err)
			}
			// Use the type name as a proxy — avoids importing each azidentity type.
			got := typeName(cred)
			if got != tt.wantType {
				t.Errorf("credential type = %q, want %q", got, tt.wantType)
			}
		})
	}
}

// ── prefixedLookuper ──────────────────────────────────────────────────────────

func TestPrefixedLookuper(t *testing.T) {
	// With prefix "AZURE_PROD_", "AZURE_SUBSCRIPTION_ID" → "AZURE_PROD_SUBSCRIPTION_ID" only —
	// no fallback to the unprefixed key.
	fakeOS := map[string]string{
		"AZURE_SUBSCRIPTION_ID":      "base-sub",   // must NOT be used when prefix is set
		"AZURE_PROD_SUBSCRIPTION_ID": "prod-sub",   // rewritten key
		"AZURE_PROD_TENANT_ID":       "prod-tenant", // rewritten key, no base equivalent
	}
	orig := envparse.OSLookuper
	envparse.OSLookuper = func(key string) (string, bool) {
		v, ok := fakeOS[key]
		return v, ok
	}
	t.Cleanup(func() { envparse.OSLookuper = orig })

	l := prefixedLookuper("AZURE_PROD_")

	t.Run("rewritten key is returned", func(t *testing.T) {
		got, ok := l("AZURE_SUBSCRIPTION_ID")
		if !ok {
			t.Fatal("expected AZURE_PROD_SUBSCRIPTION_ID to be found")
		}
		if got != "prod-sub" {
			t.Errorf("got %q, want prod-sub", got)
		}
	})

	t.Run("rewritten key found when no base key exists", func(t *testing.T) {
		got, ok := l("AZURE_TENANT_ID")
		if !ok {
			t.Fatal("expected AZURE_PROD_TENANT_ID to be found")
		}
		if got != "prod-tenant" {
			t.Errorf("got %q, want prod-tenant", got)
		}
	})

	t.Run("returns not found when rewritten key is absent — no fallback", func(t *testing.T) {
		// AZURE_RESOURCE_GROUP → AZURE_PROD_RESOURCE_GROUP (not set).
		// Must NOT fall back to AZURE_RESOURCE_GROUP.
		_, ok := l("AZURE_RESOURCE_GROUP")
		if ok {
			t.Error("expected not found — prefix lookuper must not fall back to base key")
		}
	})

	t.Run("panics on key without expected prefix", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for malformed key, got none")
			}
		}()
		l("UNEXPECTED_KEY")
	})
}

// ── Load (full — params + credential construction) ────────────────────────────

func TestProvider_Load_full(t *testing.T) {
	tests := []struct {
		name     string
		kv       map[string]string
		wantCred string
	}{
		{
			name: "service principal",
			kv: map[string]string{
				"AZURE_SUBSCRIPTION_ID": "sub-123",
				"AZURE_TENANT_ID":       "tenant-abc",
				"AZURE_CLIENT_ID":       "client-xyz",
				"AZURE_CLIENT_SECRET":   "secret",
			},
			wantCred: "*azidentity.ClientSecretCredential",
		},
		{
			name: "managed identity",
			kv: map[string]string{
				"AZURE_SUBSCRIPTION_ID": "sub-123",
				"AZURE_CLIENT_ID":       "client-xyz",
			},
			wantCred: "*azidentity.ManagedIdentityCredential",
		},
		{
			name: "default chain",
			kv: map[string]string{
				"AZURE_SUBSCRIPTION_ID": "sub-123",
			},
			wantCred: "*azidentity.DefaultAzureCredential",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := envparse.OSLookuper
			envparse.OSLookuper = func(key string) (string, bool) {
				v, ok := tt.kv[key]
				return v, ok
			}
			t.Cleanup(func() { envparse.OSLookuper = orig })

			p := &Provider{}
			if err := p.Load(nil); err != nil { //nolint:staticcheck
				t.Fatalf("Load() error = %v", err)
			}

			if p.cred == nil {
				t.Fatal("Load() left cred nil")
			}
			if got := typeName(p.cred); got != tt.wantCred {
				t.Errorf("cred type = %q, want %q", got, tt.wantCred)
			}

			// Config() must return the same credential.
			if p.Config() != p.cred {
				t.Error("Config() returned a different value than p.cred")
			}
		})
	}
}

// ── Load with prefix (full) ────────────────────────────────────────────────────

func TestProvider_Load_full_prefixed(t *testing.T) {
	fakeOS := map[string]string{
		"AZURE_PROD_SUBSCRIPTION_ID": "prod-sub",
		"AZURE_PROD_TENANT_ID":       "prod-tenant",
		"AZURE_PROD_CLIENT_ID":       "prod-client",
		"AZURE_PROD_CLIENT_SECRET":   "prod-secret",
	}
	orig := envparse.OSLookuper
	envparse.OSLookuper = func(key string) (string, bool) {
		v, ok := fakeOS[key]
		return v, ok
	}
	t.Cleanup(func() { envparse.OSLookuper = orig })

	p := NewProvider("azure-prod", "AZURE_PROD_")
	if err := p.Load(nil); err != nil { //nolint:staticcheck
		t.Fatalf("Load() error = %v", err)
	}

	if p.p.SubscriptionID != "prod-sub" {
		t.Errorf("SubscriptionID = %q, want prod-sub", p.p.SubscriptionID)
	}
	if typeName(p.cred) != "*azidentity.ClientSecretCredential" {
		t.Errorf("cred type = %q, want *azidentity.ClientSecretCredential", typeName(p.cred))
	}
}

// ── Load (params parsing only) ────────────────────────────────────────────────

func TestProvider_Load_prefix(t *testing.T) {
	// All env vars must use the prefix — no fallback.
	fakeOS := map[string]string{
		"AZURE_PROD_SUBSCRIPTION_ID": "prod-sub",
		"AZURE_PROD_RESOURCE_GROUP":  "prod-rg",
	}
	orig := envparse.OSLookuper
	envparse.OSLookuper = func(key string) (string, bool) {
		v, ok := fakeOS[key]
		return v, ok
	}
	t.Cleanup(func() { envparse.OSLookuper = orig })

	p := &Provider{prefix: "AZURE_PROD_"}
	if err := envparse.Process(&p.p, prefixedLookuper(p.prefix)); err != nil {
		t.Fatalf("Process: %v", err)
	}

	if p.p.SubscriptionID != "prod-sub" {
		t.Errorf("SubscriptionID = %q, want prod-sub", p.p.SubscriptionID)
	}
	if p.p.ResourceGroup != "prod-rg" {
		t.Errorf("ResourceGroup = %q, want prod-rg", p.p.ResourceGroup)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func typeName(v any) string {
	return fmt.Sprintf("%T", v)
}
