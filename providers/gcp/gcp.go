// Package gcp provides the built-in Google Cloud Platform provider for nimbus.
//
// Load() resolves Application Default Credentials (ADC) using the GCP auth
// library. This covers all environments without any code changes:
//
//   - GCP-hosted (GCE, GKE, Cloud Run, App Engine): metadata server
//   - Non-GCP environments (AWS, Azure, on-prem): set GOOGLE_APPLICATION_CREDENTIALS
//     to a service account key file or a Workload Identity Federation config
//   - Local development: gcloud auth application-default login
//
// The resulting [auth.Credentials] is ready to pass to any GCP client library:
//
//	creds, _ := nimbus.Retrieve[*auth.Credentials](ctx)
//	storageClient, _ := storage.NewClient(ctx, option.WithAuthCredentials(creds))
//	bigqueryClient, _ := bigquery.NewClient(ctx, projectID, option.WithAuthCredentials(creds))
//
// # Required environment variables
//
//	GCP_PROJECT_ID — GCP project identifier
//
// # Optional environment variables
//
//	GOOGLE_APPLICATION_CREDENTIALS — path to a service account JSON key file,
//	                                  or a Workload Identity Federation config
//	                                  (required when running outside GCP)
//	GCP_REGION                     — default region (e.g. us-central1)
//	GCP_ZONE                       — default zone (e.g. us-central1-a)
package gcp

import (
	"context"
	"fmt"
	"strings"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials"
	"github.com/victorialuquet/nimbus/internal/envparse"
)

// params holds the raw environment inputs.
// It is not exported — callers interact with [auth.Credentials] directly.
type params struct {
	ProjectID string `env:"GCP_PROJECT_ID,required"`
	Region    string `env:"GCP_REGION"`
	Zone      string `env:"GCP_ZONE"`
}

// Provider implements [provider.Provider] for GCP.
// Config() returns [*auth.Credentials] ready for use with any GCP client library.
//
// For multi-project deployments, use [NewProvider] with a name and env var
// prefix so each instance reads from its own set of environment variables:
//
//	prod := gcp.NewProvider("gcp-prod", "GCP_PROD_")
//	staging := gcp.NewProvider("gcp-staging", "GCP_STAGING_")
//	// reads GCP_PROD_PROJECT_ID, GCP_STAGING_PROJECT_ID, etc.
//
// Note: GOOGLE_APPLICATION_CREDENTIALS is a GCP SDK standard and is not
// prefixed — each project instance shares the same credential file or ADC.
// For separate credentials per project, use distinct service account files
// and set GOOGLE_APPLICATION_CREDENTIALS before each process.
type Provider struct {
	p      params
	creds  *auth.Credentials
	name   string
	prefix string
}

// NewProvider creates a GCP provider with a custom name and an optional env
// var prefix for multi-project deployments.
//
// When prefix is non-empty, prefixed vars take priority with fallback to the
// unprefixed name. For example, with prefix "GCP_PROD_", the project ID is
// read from GCP_PROD_PROJECT_ID, falling back to GCP_PROJECT_ID.
//
//	prod := gcp.NewProvider("gcp-prod", "GCP_PROD_")
//	ctx, _ = nimbus.SetupProviders(ctx, provider.WithProviders(prod))
func NewProvider(name, prefix string) *Provider {
	return &Provider{name: name, prefix: prefix}
}

// Name returns the provider's registered name (default: "gcp").
func (p *Provider) Name() string {
	if p.name != "" {
		return p.name
	}
	return "gcp"
}

// prefixedLookuper returns a Lookuper that rewrites keys using the given
// prefix. Keys in the GCP provider's env tags follow the pattern "GCP_*".
// With prefix "GCP_PROD_", the key "GCP_PROJECT_ID" is rewritten exclusively
// to "GCP_PROD_PROJECT_ID" — no fallback to the unprefixed key.
//
// This is intentional: if a prefix is declared, every lookup is scoped to
// that prefix. Silent fallback to the base env vars would risk using the wrong
// project without any warning.
func prefixedLookuper(prefix string) envparse.Lookuper {
	const base = "GCP_"
	return func(key string) (string, bool) {
		after, ok := strings.CutPrefix(key, base)
		if !ok {
			panic("gcp: prefixedLookuper: unexpected key format: " + key)
		}
		return envparse.OSLookuper(prefix + after)
	}
}

// Load resolves GCP Application Default Credentials and stores them for
// retrieval via Config().
//
// Credential resolution order (handled by the GCP auth library):
//  1. GOOGLE_APPLICATION_CREDENTIALS — service account key or WIF config
//  2. gcloud application-default credentials (~/.config/gcloud/...)
//  3. GCP metadata server (when running on GCP infrastructure)
//
// When running on AWS or Azure, point GOOGLE_APPLICATION_CREDENTIALS to a
// Workload Identity Federation configuration file. No code changes needed —
// the auth library handles token exchange transparently.
//
// When a prefix is set (via [NewProvider]), prefixed vars take priority:
// e.g. GCP_PROD_PROJECT_ID is checked before GCP_PROJECT_ID.
func (p *Provider) Load(ctx context.Context) error {
	lookup := envparse.Lookuper(nil)
	if p.prefix != "" {
		lookup = prefixedLookuper(p.prefix)
	}
	if err := envparse.Process(&p.p, lookup); err != nil {
		return fmt.Errorf("gcp: %w", err)
	}

	creds, err := credentials.DetectDefault(&credentials.DetectOptions{
		// cloud-platform is the broadest scope; use narrower scopes in
		// production if you want to restrict what the credentials can access.
		Scopes: []string{"https://www.googleapis.com/auth/cloud-platform"},
	})
	if err != nil {
		return fmt.Errorf("gcp: failed to detect credentials: %w", err)
	}

	p.creds = creds
	return nil
}

// Validate runs struct-level validation on the loaded params.
func (p *Provider) Validate() error {
	if p.p.ProjectID == "" {
		return fmt.Errorf("gcp: GCP_PROJECT_ID is required")
	}
	return nil
}

// Config returns the [*auth.Credentials] resolved during Load.
// Pass it to any GCP client library via option.WithAuthCredentials:
//
//	creds, _ := nimbus.Retrieve[*auth.Credentials](ctx)
//	client, _ := storage.NewClient(ctx, option.WithAuthCredentials(creds))
func (p *Provider) Config() any {
	return p.creds
}
