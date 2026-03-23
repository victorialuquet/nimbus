package aws

import (
	"context"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/victorialuquet/nimbus/internal/envparse"
)

// env builds a Lookuper from a static map — no os.Environ mutation needed.
func env(kv map[string]string) envparse.Lookuper {
	return func(key string) (string, bool) {
		v, ok := kv[key]
		return v, ok
	}
}

// loadWithEnv populates a Provider's params using an injected lookuper,
// without going through the full AWS SDK config construction.
// Used to test parsing and validation in isolation.
func loadWithEnv(p *Provider, kv map[string]string) error {
	return envparse.Process(&p.p, env(kv))
}

// ── Name ─────────────────────────────────────────────────────────────────────

func TestProvider_Name_default(t *testing.T) {
	p := &Provider{}
	if got := p.Name(); got != "aws" {
		t.Errorf("Name() = %q, want %q", got, "aws")
	}
}

func TestProvider_Name_custom(t *testing.T) {
	p := NewProvider("aws-eu", "")
	if got := p.Name(); got != "aws-eu" {
		t.Errorf("Name() = %q, want %q", got, "aws-eu")
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
			name: "no credentials — SDK default chain",
			kv:   map[string]string{"AWS_REGION": "us-east-1"},
		},
		{
			name: "static credentials — both key and secret",
			kv: map[string]string{
				"AWS_REGION":            "us-east-1",
				"AWS_ACCESS_KEY_ID":     "AKIAIOSFODNN7EXAMPLE",
				"AWS_SECRET_ACCESS_KEY": "secret",
			},
		},
		{
			name: "role ARN only",
			kv: map[string]string{
				"AWS_REGION":   "us-east-1",
				"AWS_ROLE_ARN": "arn:aws:iam::123456789012:role/MyRole",
			},
		},
		{
			name: "key without secret — should error",
			kv: map[string]string{
				"AWS_REGION":        "us-east-1",
				"AWS_ACCESS_KEY_ID": "AKIAIOSFODNN7EXAMPLE",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Provider{}
			if err := loadWithEnv(p, tt.kv); err != nil {
				// required field missing — not Validate's concern, skip
				if !tt.wantErr {
					t.Fatalf("loadWithEnv: unexpected error: %v", err)
				}
				return
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
	// The lookuper receives raw env tag keys like "AWS_REGION".
	// With prefix "AWS_EU_", it rewrites "AWS_REGION" → "AWS_EU_REGION" only —
	// no fallback to the unprefixed key.
	fakeEnv := map[string]string{
		"AWS_REGION":           "us-east-1", // must NOT be used when prefix is set
		"AWS_EU_REGION":        "eu-west-1", // rewritten key
		"AWS_EU_ACCESS_KEY_ID": "eu-key",    // rewritten key, no base equivalent
	}
	orig := envparse.OSLookuper
	envparse.OSLookuper = func(key string) (string, bool) {
		v, ok := fakeEnv[key]
		return v, ok
	}
	t.Cleanup(func() { envparse.OSLookuper = orig })

	l := prefixedLookuper("AWS_EU_")

	t.Run("rewritten key is returned", func(t *testing.T) {
		got, ok := l("AWS_REGION")
		if !ok {
			t.Fatal("expected AWS_EU_REGION to be found")
		}
		if got != "eu-west-1" {
			t.Errorf("got %q, want eu-west-1", got)
		}
	})

	t.Run("rewritten key found when no base key exists", func(t *testing.T) {
		got, ok := l("AWS_ACCESS_KEY_ID")
		if !ok {
			t.Fatal("expected AWS_EU_ACCESS_KEY_ID to be found")
		}
		if got != "eu-key" {
			t.Errorf("got %q, want eu-key", got)
		}
	})

	t.Run("returns not found when rewritten key is absent — no fallback", func(t *testing.T) {
		// AWS_SECRET_ACCESS_KEY → AWS_EU_SECRET_ACCESS_KEY (not set).
		// Must NOT fall back to AWS_SECRET_ACCESS_KEY.
		_, ok := l("AWS_SECRET_ACCESS_KEY")
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

// ── Load (params parsing only) ────────────────────────────────────────────────

func TestProvider_Load_params(t *testing.T) {
	tests := []struct {
		name       string
		kv         map[string]string
		wantRegion string
		wantErr    bool
	}{
		{
			name:       "region set",
			kv:         map[string]string{"AWS_REGION": "ap-southeast-1"},
			wantRegion: "ap-southeast-1",
		},
		{
			name:    "region missing — required",
			kv:      map[string]string{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Provider{}
			err := loadWithEnv(p, tt.kv)
			if (err != nil) != tt.wantErr {
				t.Fatalf("loadWithEnv() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && p.p.Region != tt.wantRegion {
				t.Errorf("Region = %q, want %q", p.p.Region, tt.wantRegion)
			}
		})
	}
}

func TestProvider_Load_prefix(t *testing.T) {
	// All env vars must use the prefix — no fallback.
	fakeEnv := map[string]string{
		"AWS_EU_REGION":            "eu-west-1",
		"AWS_EU_ACCESS_KEY_ID":     "eu-key",
		"AWS_EU_SECRET_ACCESS_KEY": "eu-secret",
	}
	orig := envparse.OSLookuper
	envparse.OSLookuper = func(key string) (string, bool) {
		v, ok := fakeEnv[key]
		return v, ok
	}
	t.Cleanup(func() { envparse.OSLookuper = orig })

	p := &Provider{prefix: "AWS_EU_"}
	if err := envparse.Process(&p.p, prefixedLookuper(p.prefix)); err != nil {
		t.Fatalf("Process: %v", err)
	}

	if p.p.Region != "eu-west-1" {
		t.Errorf("Region = %q, want eu-west-1", p.p.Region)
	}
	if p.p.AccessKeyID != "eu-key" {
		t.Errorf("AccessKeyID = %q, want eu-key", p.p.AccessKeyID)
	}
}

// ── Load (full SDK config) ────────────────────────────────────────────────────

func TestProvider_Load_sdkConfig(t *testing.T) {
	// Patch OSLookuper so Load() reads from our fake env.
	orig := envparse.OSLookuper
	envparse.OSLookuper = func(key string) (string, bool) {
		kv := map[string]string{
			"AWS_REGION":            "sa-east-1",
			"AWS_ACCESS_KEY_ID":     "AKIAIOSFODNN7EXAMPLE",
			"AWS_SECRET_ACCESS_KEY": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		}
		v, ok := kv[key]
		return v, ok
	}
	t.Cleanup(func() { envparse.OSLookuper = orig })

	p := &Provider{}
	if err := p.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if p.sdkCfg.Region != "sa-east-1" {
		t.Errorf("sdkCfg.Region = %q, want sa-east-1", p.sdkCfg.Region)
	}

	cfg, ok := p.Config().(awssdk.Config)
	if !ok {
		t.Fatal("Config() did not return aws.Config")
	}
	if cfg.Region != "sa-east-1" {
		t.Errorf("Config().Region = %q, want sa-east-1", cfg.Region)
	}
}

func TestProvider_Load_withProfile(t *testing.T) {
	orig := envparse.OSLookuper
	envparse.OSLookuper = func(key string) (string, bool) {
		kv := map[string]string{
			"AWS_REGION":  "eu-central-1",
			"AWS_PROFILE": "myprofile",
		}
		v, ok := kv[key]
		return v, ok
	}
	t.Cleanup(func() { envparse.OSLookuper = orig })

	p := &Provider{}
	// Load will call config.WithSharedConfigProfile — it may fail if the profile
	// doesn't exist on disk, but the branch is still exercised.
	_ = p.Load(context.Background())

	if p.p.Profile != "myprofile" {
		t.Errorf("p.p.Profile = %q, want myprofile", p.p.Profile)
	}
}

func TestProvider_Load_withEndpoint(t *testing.T) {
	orig := envparse.OSLookuper
	envparse.OSLookuper = func(key string) (string, bool) {
		kv := map[string]string{
			"AWS_REGION":            "us-east-1",
			"AWS_ACCESS_KEY_ID":     "test",
			"AWS_SECRET_ACCESS_KEY": "test",
			"AWS_ENDPOINT":          "http://localhost:4566",
		}
		v, ok := kv[key]
		return v, ok
	}
	t.Cleanup(func() { envparse.OSLookuper = orig })

	p := &Provider{}
	if err := p.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if p.p.Endpoint != "http://localhost:4566" {
		t.Errorf("p.p.Endpoint = %q, want http://localhost:4566", p.p.Endpoint)
	}
	if p.sdkCfg.Region != "us-east-1" {
		t.Errorf("sdkCfg.Region = %q, want us-east-1", p.sdkCfg.Region)
	}
}
