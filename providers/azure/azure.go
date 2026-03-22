// Package azure provides the built-in Microsoft Azure provider for nimbus.
//
// Load() builds the most specific [azcore.TokenCredential] supported by the
// configured environment. All Azure SDK clients accept this interface, so the
// same credential works across ARM, storage, key vault, and any other service:
//
//	cred, _ := nimbus.Retrieve[azcore.TokenCredential](ctx)
//	client, _ := armresources.NewClient(subscriptionID, cred, nil)
//	blobClient, _ := azblob.NewClient(url, cred, nil)
//
// # Credential resolution order
//
//  1. Service principal — when AZURE_CLIENT_ID + AZURE_CLIENT_SECRET + AZURE_TENANT_ID are set.
//     Use this when running outside Azure (AWS, GCP, on-prem).
//  2. Managed identity — when only AZURE_CLIENT_ID is set (no secret).
//     Covers Azure VMs, AKS pods, App Service, etc. with user-assigned identity.
//  3. Default chain — when no client credentials are set.
//     Tries managed identity (system-assigned), Azure CLI, environment vars,
//     and other developer flows in order.
//
// # Required environment variables
//
//	AZURE_SUBSCRIPTION_ID — Azure subscription identifier
//
// # Optional environment variables
//
//	AZURE_TENANT_ID       — Azure Active Directory tenant (required for service principal)
//	AZURE_CLIENT_ID       — Service principal or managed identity client ID
//	AZURE_CLIENT_SECRET   — Service principal secret (requires AZURE_TENANT_ID)
//	AZURE_RESOURCE_GROUP  — Default resource group
//	AZURE_LOCATION        — Default Azure region (e.g. eastus)
package azure

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/victorialuquet/nimbus/internal/envparse"
)

// params holds the raw environment inputs.
// It is not exported — callers interact with [azcore.TokenCredential] directly.
type params struct {
	SubscriptionID string `env:"AZURE_SUBSCRIPTION_ID,required"`
	TenantID       string `env:"AZURE_TENANT_ID"`
	ClientID       string `env:"AZURE_CLIENT_ID"`
	ClientSecret   string `env:"AZURE_CLIENT_SECRET"`
	ResourceGroup  string `env:"AZURE_RESOURCE_GROUP"`
	Location       string `env:"AZURE_LOCATION"`
}

// Provider implements [provider.Provider] for Azure.
// Config() returns an [azcore.TokenCredential] ready for use with any Azure SDK client.
//
// For multi-tenant or multi-subscription deployments, use [NewProvider] with
// a name and env var prefix so each instance reads from its own set of
// environment variables:
//
//	prod := azure.NewProvider("azure-prod", "AZURE_PROD_")
//	staging := azure.NewProvider("azure-staging", "AZURE_STAGING_")
//	// reads AZURE_PROD_SUBSCRIPTION_ID, AZURE_STAGING_SUBSCRIPTION_ID, etc.
//
// Note: AZURE_TENANT_ID, AZURE_CLIENT_ID, and AZURE_CLIENT_SECRET are
// also prefixed, so separate service principals per environment are supported.
type Provider struct {
	p      params
	cred   azcore.TokenCredential
	name   string
	prefix string
}

// NewProvider creates an Azure provider with a custom name and an optional
// env var prefix for multi-tenant or multi-subscription deployments.
//
// When prefix is non-empty, prefixed vars take priority with fallback to the
// unprefixed name. For example, with prefix "AZURE_PROD_", the subscription
// is read from AZURE_PROD_SUBSCRIPTION_ID, falling back to AZURE_SUBSCRIPTION_ID.
//
//	prod := azure.NewProvider("azure-prod", "AZURE_PROD_")
//	ctx, _ = nimbus.SetupProviders(ctx, provider.WithProviders(prod))
func NewProvider(name, prefix string) *Provider {
	return &Provider{name: name, prefix: prefix}
}

// Name returns the provider's registered name (default: "azure").
func (p *Provider) Name() string {
	if p.name != "" {
		return p.name
	}
	return "azure"
}

// prefixedLookuper returns a Lookuper that rewrites keys using the given
// prefix. Keys in the Azure provider's env tags follow the pattern "AZURE_*".
// With prefix "AZURE_PROD_", the key "AZURE_SUBSCRIPTION_ID" is rewritten
// exclusively to "AZURE_PROD_SUBSCRIPTION_ID" — no fallback to the unprefixed
// key.
//
// This is intentional: if a prefix is declared, every lookup is scoped to
// that prefix. Silent fallback to the base env vars would risk using the wrong
// subscription or service principal without any warning.
func prefixedLookuper(prefix string) envparse.Lookuper {
	const base = "AZURE_"
	return func(key string) (string, bool) {
		after, ok := strings.CutPrefix(key, base)
		if !ok {
			panic("azure: prefixedLookuper: unexpected key format: " + key)
		}
		return envparse.OSLookuper(prefix + after)
	}
}

// Load parses Azure environment variables and builds the appropriate
// [azcore.TokenCredential] based on the credentials present.
//
// When a prefix is set (via [NewProvider]), prefixed vars take priority:
// e.g. AZURE_PROD_SUBSCRIPTION_ID is checked before AZURE_SUBSCRIPTION_ID.
func (p *Provider) Load(_ context.Context) error {
	lookup := envparse.Lookuper(nil)
	if p.prefix != "" {
		lookup = prefixedLookuper(p.prefix)
	}
	if err := envparse.Process(&p.p, lookup); err != nil {
		return fmt.Errorf("azure: %w", err)
	}

	cred, err := p.buildCredential()
	if err != nil {
		return fmt.Errorf("azure: %w", err)
	}

	p.cred = cred
	return nil
}

// buildCredential selects the most specific credential type available.
func (p *Provider) buildCredential() (azcore.TokenCredential, error) {
	hasServicePrincipal := p.p.ClientID != "" && p.p.ClientSecret != "" && p.p.TenantID != ""
	hasManagedIdentity := p.p.ClientID != "" && p.p.ClientSecret == ""

	switch {
	case hasServicePrincipal:
		// Explicit service principal — use when running outside Azure.
		return azidentity.NewClientSecretCredential(
			p.p.TenantID,
			p.p.ClientID,
			p.p.ClientSecret,
			nil,
		)

	case hasManagedIdentity:
		// User-assigned managed identity — running on Azure with a specific identity.
		return azidentity.NewManagedIdentityCredential(&azidentity.ManagedIdentityCredentialOptions{
			ID: azidentity.ClientID(p.p.ClientID),
		})

	default:
		// Default chain: system-assigned managed identity → Azure CLI → env vars.
		// Covers local development and Azure-hosted workloads without explicit config.
		return azidentity.NewDefaultAzureCredential(nil)
	}
}

// Validate checks credential consistency.
func (p *Provider) Validate() error {
	if p.p.ClientSecret != "" && p.p.ClientID == "" {
		return fmt.Errorf("azure: AZURE_CLIENT_SECRET set but AZURE_CLIENT_ID is missing")
	}
	if p.p.ClientSecret != "" && p.p.TenantID == "" {
		return fmt.Errorf("azure: service principal auth requires AZURE_TENANT_ID")
	}
	return nil
}

// Config returns the [azcore.TokenCredential] built during Load.
// Pass it directly to any Azure SDK client constructor:
//
//	cred, _ := nimbus.Retrieve[azcore.TokenCredential](ctx)
//	client, _ := armresources.NewClient(subscriptionID, cred, nil)
func (p *Provider) Config() any {
	return p.cred
}
