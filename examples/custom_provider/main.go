// Package main demonstrates implementing a custom [provider.Provider] for a
// third-party service (here: a fictional "Vault" secrets backend).
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/victorialuquet/nimbus"
	"github.com/victorialuquet/nimbus/internal/envparse"
	"github.com/victorialuquet/nimbus/provider"
)

// VaultConfig holds configuration for a HashiCorp Vault instance.
type VaultConfig struct {
	Address   string `env:"VAULT_ADDR,required"  validate:"required,url"`
	Token     string `env:"VAULT_TOKEN"`
	RoleID    string `env:"VAULT_ROLE_ID"`
	SecretID  string `env:"VAULT_SECRET_ID"`
	Namespace string `env:"VAULT_NAMESPACE,default=default"`
}

// VaultProvider is a custom provider that satisfies [provider.Provider].
type VaultProvider struct {
	cfg VaultConfig
}

func (p *VaultProvider) Name() string { return "vault" }

func (p *VaultProvider) Load(_ context.Context) error {
	return envparse.Process(&p.cfg, nil)
}

func (p *VaultProvider) Validate() error {
	hasToken := p.cfg.Token != ""
	hasAppRole := p.cfg.RoleID != "" && p.cfg.SecretID != ""
	if !hasToken && !hasAppRole {
		return fmt.Errorf("vault: authentication required — set VAULT_TOKEN or VAULT_ROLE_ID+VAULT_SECRET_ID")
	}
	return nil
}

func (p *VaultProvider) Config() any { return &p.cfg }

// Ping implements [provider.Observable] — optional connectivity check.
func (p *VaultProvider) Ping(_ context.Context) error {
	// In a real implementation: HTTP GET p.cfg.Address/v1/sys/health
	fmt.Println("vault: ping OK")
	return nil
}

func main() {
	os.Setenv("PROVIDERS", "vault")
	os.Setenv("VAULT_ADDR", "https://vault.example.com")
	os.Setenv("VAULT_TOKEN", "s.abc123")

	ctx := context.Background()

	ctx, err := nimbus.SetupProviders(ctx,
		provider.WithProviders(&VaultProvider{}),
		provider.WithPing(),
	)
	if err != nil {
		log.Fatalf("providers: %v", err)
	}

	vaultCfg, err := nimbus.Retrieve[*VaultConfig](ctx)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Vault address: %s (namespace: %s)\n", vaultCfg.Address, vaultCfg.Namespace)
}
