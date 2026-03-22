// Package aws provides the built-in AWS cloud provider for nimbus.
//
// Load() builds an [aws.Config] from the environment using the AWS SDK default
// credential chain. Static credentials, IAM role assumption, named profiles,
// and instance/task roles are all supported — the same mechanisms the AWS SDK
// itself understands.
//
// The resulting [aws.Config] is ready to pass directly to any AWS service
// client:
//
//	cfg, _ := nimbus.Retrieve[aws.Config](ctx)
//	s3Client := s3.NewFromConfig(cfg)
//	dynamoClient := dynamodb.NewFromConfig(cfg)
//
// # Required environment variables
//
//	AWS_REGION — AWS region (e.g. us-east-1)
//
// # Optional environment variables
//
//	AWS_ACCESS_KEY_ID     — static credentials (key)
//	AWS_SECRET_ACCESS_KEY — static credentials (secret)
//	AWS_ROLE_ARN          — IAM role to assume
//	AWS_ENDPOINT          — override endpoint URL (useful for LocalStack)
//	AWS_PROFILE           — named profile from ~/.aws/credentials
package aws

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/victorialuquet/nimbus/internal/envparse"
)

// params holds the raw environment inputs used to build the SDK config.
// It is not exported — callers interact with [aws.Config] directly.
type params struct {
	Region          string `env:"AWS_REGION,required"`
	AccessKeyID     string `env:"AWS_ACCESS_KEY_ID"`
	SecretAccessKey string `env:"AWS_SECRET_ACCESS_KEY"`
	RoleARN         string `env:"AWS_ROLE_ARN"`
	Endpoint        string `env:"AWS_ENDPOINT"`
	Profile         string `env:"AWS_PROFILE"`
}

// Provider implements [provider.Provider] for AWS.
// Config() returns an [aws.Config] ready for use with any AWS service client.
//
// For multi-region or multi-account deployments, use [NewProvider] with a
// name and env var prefix so each instance reads from its own set of
// environment variables:
//
//	us := aws.NewProvider("aws-us", "AWS_US_")
//	eu := aws.NewProvider("aws-eu", "AWS_EU_")
//	// reads AWS_US_REGION, AWS_EU_REGION, etc.
type Provider struct {
	p      params
	sdkCfg aws.Config
	name   string
	prefix string
}

// NewProvider creates an AWS provider with a custom name and an optional env
// var prefix, enabling multi-region or multi-account deployments.
//
// When prefix is non-empty, all env var lookups are attempted with the prefix
// first, falling back to the unprefixed name. For example, with prefix
// "AWS_EU_", the region is read from AWS_EU_REGION, falling back to
// AWS_REGION.
//
//	eu := aws.NewProvider("aws-eu", "AWS_EU_")
//	ctx, _ = nimbus.SetupProviders(ctx, provider.WithProviders(eu))
func NewProvider(name, prefix string) *Provider {
	return &Provider{name: name, prefix: prefix}
}

// Name returns the provider's registered name (default: "aws").
func (p *Provider) Name() string {
	if p.name != "" {
		return p.name
	}
	return "aws"
}

// prefixedLookuper returns a Lookuper that rewrites keys using the given
// prefix. Keys in the AWS provider's env tags follow the pattern "AWS_*".
// With prefix "AWS_EU_", the key "AWS_REGION" is rewritten exclusively to
// "AWS_EU_REGION" — no fallback to the unprefixed key.
//
// This is intentional: if a prefix is declared, every lookup is scoped to
// that prefix. Silent fallback to the base env vars would risk loading
// credentials from the wrong account without any warning.
// Required fields that are absent with the prefix will produce a clear error
// from envparse.
func prefixedLookuper(prefix string) envparse.Lookuper {
	const base = "AWS_"
	return func(key string) (string, bool) {
		after, ok := strings.CutPrefix(key, base)
		if !ok {
			// Key does not start with the expected base — should not happen
			// with well-formed env tags, but be safe.
			panic("aws: prefixedLookuper: unexpected key format: " + key)
		}
		return envparse.OSLookuper(prefix + after)
	}
}

// Load parses AWS environment variables and builds the [aws.Config].
//
// Credential resolution order:
//  1. Static credentials (AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY)
//  2. Named profile (AWS_PROFILE)
//  3. SDK default chain (env vars, ~/.aws/credentials, instance/task role, etc.)
//
// AWS_ROLE_ARN is picked up by the SDK default chain automatically.
// When a prefix is set (via [NewProvider]), prefixed vars take priority:
// e.g. AWS_EU_REGION is checked before AWS_REGION.
func (p *Provider) Load(ctx context.Context) error {
	lookup := envparse.Lookuper(nil)
	if p.prefix != "" {
		lookup = prefixedLookuper(p.prefix)
	}
	if err := envparse.Process(&p.p, lookup); err != nil {
		return fmt.Errorf("aws: %w", err)
	}

	opts := []func(*config.LoadOptions) error{
		config.WithRegion(p.p.Region),
	}

	if p.p.AccessKeyID != "" && p.p.SecretAccessKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				p.p.AccessKeyID,
				p.p.SecretAccessKey,
				"",
			),
		))
	}

	if p.p.Profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(p.p.Profile))
	}

	if p.p.Endpoint != "" {
		opts = append(opts, config.WithBaseEndpoint(p.p.Endpoint))
	}

	sdkCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return fmt.Errorf("aws: failed to build sdk config: %w", err)
	}

	p.sdkCfg = sdkCfg
	return nil
}

// Validate checks credential consistency before the SDK config is used.
func (p *Provider) Validate() error {
	if p.p.AccessKeyID != "" && p.p.SecretAccessKey == "" {
		return fmt.Errorf("aws: AWS_ACCESS_KEY_ID set but AWS_SECRET_ACCESS_KEY is missing")
	}
	return nil
}

// Config returns the [aws.Config] built during Load.
// Pass it directly to any AWS service client constructor:
//
//	cfg, _ := nimbus.Retrieve[aws.Config](ctx)
//	client := s3.NewFromConfig(cfg)
func (p *Provider) Config() any {
	return p.sdkCfg
}
