package gcp

import (
	"context"
	"os"
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
// without triggering ADC resolution (which requires I/O).
func loadParams(p *Provider, kv map[string]string) error {
	return envparse.Process(&p.p, fakeEnv(kv))
}

// ── Name ─────────────────────────────────────────────────────────────────────

func TestProvider_Name_default(t *testing.T) {
	p := &Provider{}
	if got := p.Name(); got != "gcp" {
		t.Errorf("Name() = %q, want %q", got, "gcp")
	}
}

func TestProvider_Name_custom(t *testing.T) {
	p := NewProvider("gcp-prod", "")
	if got := p.Name(); got != "gcp-prod" {
		t.Errorf("Name() = %q, want %q", got, "gcp-prod")
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
			name: "project ID set",
			kv:   map[string]string{"GCP_PROJECT_ID": "my-project"},
		},
		{
			name:    "project ID missing",
			kv:      map[string]string{},
			wantErr: true,
		},
		{
			name: "project ID with optional fields",
			kv: map[string]string{
				"GCP_PROJECT_ID": "my-project",
				"GCP_REGION":     "us-central1",
				"GCP_ZONE":       "us-central1-a",
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

// ── prefixedLookuper ──────────────────────────────────────────────────────────

func TestPrefixedLookuper(t *testing.T) {
	// With prefix "GCP_PROD_", key "GCP_PROJECT_ID" → "GCP_PROD_PROJECT_ID" only —
	// no fallback to the unprefixed key.
	fakeOS := map[string]string{
		"GCP_PROJECT_ID":      "base-project", // must NOT be used when prefix is set
		"GCP_PROD_PROJECT_ID": "prod-project", // rewritten key
		"GCP_PROD_REGION":     "europe-west1", // rewritten key, no base equivalent
	}
	orig := envparse.OSLookuper
	envparse.OSLookuper = func(key string) (string, bool) {
		v, ok := fakeOS[key]
		return v, ok
	}
	t.Cleanup(func() { envparse.OSLookuper = orig })

	l := prefixedLookuper("GCP_PROD_")

	t.Run("rewritten key is returned", func(t *testing.T) {
		got, ok := l("GCP_PROJECT_ID")
		if !ok {
			t.Fatal("expected GCP_PROD_PROJECT_ID to be found")
		}
		if got != "prod-project" {
			t.Errorf("got %q, want prod-project", got)
		}
	})

	t.Run("rewritten key found when no base key exists", func(t *testing.T) {
		got, ok := l("GCP_REGION")
		if !ok {
			t.Fatal("expected GCP_PROD_REGION to be found")
		}
		if got != "europe-west1" {
			t.Errorf("got %q, want europe-west1", got)
		}
	})

	t.Run("returns not found when rewritten key is absent — no fallback", func(t *testing.T) {
		// GCP_ZONE → GCP_PROD_ZONE (not set). Must NOT fall back to GCP_ZONE.
		_, ok := l("GCP_ZONE")
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

// ── Load (full — params + ADC stub via GOOGLE_APPLICATION_CREDENTIALS) ────────

func TestProvider_Load_full(t *testing.T) {
	// Write a minimal service-account JSON to a temp file so credentials.DetectDefault
	// picks it up via GOOGLE_APPLICATION_CREDENTIALS without real network I/O.
	saJSON := `{
		"type": "service_account",
		"project_id": "test-project",
		"private_key_id": "key1",
		"private_key": "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA2a2rwplBQLzHPZe5TSd1j9DKhEzs1XGQokiJsFkRPMQQrpLn\noJKRVMhFTDkEjFj7sBoAEDMtCpxehCCXmtRMG2DKmhKCEuZUDDkFEF/iq2XBsHXq\nAMMJVHzPxHMWYAMuZLPHFqGDZmVxGJBBDEVcM3GnINdweBZhT0FBGn5YO8KnIWFQ\nj3V8ym2G0K4C7X3p6HKyX6VxKZ/xYbUTf3H5v3eMeECe6jXEtb3b0pJCvjpAK9J\nP9X1Bi/o2fBoLLVbwp5gCr3C6MOI7Mn1uyBMkDfN1LmUJTjJc2RMqN5yYCv9fNL8\n3bOFo9L1DGCkMYWkPT9QKSO2GPVQQ+Ly/+6LFwIDAQABAoIBAHjCPkTRVFxqSCGE\nxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n-----END RSA PRIVATE KEY-----\n",
		"client_email": "test@test-project.iam.gserviceaccount.com",
		"client_id": "123456789",
		"auth_uri": "https://accounts.google.com/o/oauth2/auth",
		"token_uri": "https://oauth2.googleapis.com/token"
	}`

	dir := t.TempDir()
	keyFile := dir + "/sa.json"
	if err := os.WriteFile(keyFile, []byte(saJSON), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	orig := envparse.OSLookuper
	envparse.OSLookuper = func(key string) (string, bool) {
		kv := map[string]string{
			"GCP_PROJECT_ID":               "test-project",
			"GOOGLE_APPLICATION_CREDENTIALS": keyFile,
		}
		v, ok := kv[key]
		return v, ok
	}
	t.Cleanup(func() { envparse.OSLookuper = orig })

	// Also set the real env var so the GCP auth library can find the file.
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", keyFile)

	p := &Provider{}
	if err := p.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if p.creds == nil {
		t.Fatal("Load() left creds nil")
	}

	// Config() must return the credentials.
	if p.Config() != p.creds {
		t.Error("Config() returned a different value than p.creds")
	}
}

// ── Load with prefix (full) ────────────────────────────────────────────────────

func TestProvider_Load_full_prefixed(t *testing.T) {
	dir := t.TempDir()
	keyFile := dir + "/sa.json"
	saJSON := `{
		"type": "service_account",
		"project_id": "prod-project",
		"private_key_id": "key1",
		"private_key": "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA2a2rwplBQLzHPZe5TSd1j9DKhEzs1XGQokiJsFkRPMQQrpLn\noJKRVMhFTDkEjFj7sBoAEDMtCpxehCCXmtRMG2DKmhKCEuZUDDkFEF/iq2XBsHXq\nAMMJVHzPxHMWYAMuZLPHFqGDZmVxGJBBDEVcM3GnINdweBZhT0FBGn5YO8KnIWFQ\nj3V8ym2G0K4C7X3p6HKyX6VxKZ/xYbUTf3H5v3eMeECe6jXEtb3b0pJCvjpAK9J\nP9X1Bi/o2fBoLLVbwp5gCr3C6MOI7Mn1uyBMkDfN1LmUJTjJc2RMqN5yYCv9fNL8\n3bOFo9L1DGCkMYWkPT9QKSO2GPVQQ+Ly/+6LFwIDAQABAoIBAHjCPkTRVFxqSCGE\nxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n-----END RSA PRIVATE KEY-----\n",
		"client_email": "test@prod-project.iam.gserviceaccount.com",
		"client_id": "123456789",
		"auth_uri": "https://accounts.google.com/o/oauth2/auth",
		"token_uri": "https://oauth2.googleapis.com/token"
	}`
	if err := os.WriteFile(keyFile, []byte(saJSON), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	fakeOS := map[string]string{
		"GCP_PROD_PROJECT_ID": "prod-project",
	}
	orig := envparse.OSLookuper
	envparse.OSLookuper = func(key string) (string, bool) {
		v, ok := fakeOS[key]
		return v, ok
	}
	t.Cleanup(func() { envparse.OSLookuper = orig })

	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", keyFile)

	p := NewProvider("gcp-prod", "GCP_PROD_")
	if err := p.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if p.p.ProjectID != "prod-project" {
		t.Errorf("ProjectID = %q, want prod-project", p.p.ProjectID)
	}
	if p.creds == nil {
		t.Fatal("Load() left creds nil")
	}
}

// ── Load (params parsing only) ────────────────────────────────────────────────

func TestProvider_Load_params(t *testing.T) {
	tests := []struct {
		name          string
		kv            map[string]string
		wantProjectID string
		wantRegion    string
		wantErr       bool
	}{
		{
			name:          "required field set",
			kv:            map[string]string{"GCP_PROJECT_ID": "my-project"},
			wantProjectID: "my-project",
		},
		{
			name: "all fields set",
			kv: map[string]string{
				"GCP_PROJECT_ID": "full-project",
				"GCP_REGION":     "us-central1",
				"GCP_ZONE":       "us-central1-a",
			},
			wantProjectID: "full-project",
			wantRegion:    "us-central1",
		},
		{
			name:    "required field missing",
			kv:      map[string]string{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Provider{}
			err := loadParams(p, tt.kv)
			if (err != nil) != tt.wantErr {
				t.Fatalf("loadParams() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if p.p.ProjectID != tt.wantProjectID {
				t.Errorf("ProjectID = %q, want %q", p.p.ProjectID, tt.wantProjectID)
			}
			if tt.wantRegion != "" && p.p.Region != tt.wantRegion {
				t.Errorf("Region = %q, want %q", p.p.Region, tt.wantRegion)
			}
		})
	}
}

func TestProvider_Load_prefix(t *testing.T) {
	// All env vars must use the prefix — no fallback.
	fakeOS := map[string]string{
		"GCP_PROD_PROJECT_ID": "prod-project",
		"GCP_PROD_REGION":     "europe-west1",
	}
	orig := envparse.OSLookuper
	envparse.OSLookuper = func(key string) (string, bool) {
		v, ok := fakeOS[key]
		return v, ok
	}
	t.Cleanup(func() { envparse.OSLookuper = orig })

	p := &Provider{prefix: "GCP_PROD_"}
	if err := envparse.Process(&p.p, prefixedLookuper(p.prefix)); err != nil {
		t.Fatalf("Process: %v", err)
	}

	if p.p.ProjectID != "prod-project" {
		t.Errorf("ProjectID = %q, want prod-project", p.p.ProjectID)
	}
	if p.p.Region != "europe-west1" {
		t.Errorf("Region = %q, want europe-west1", p.p.Region)
	}
}
