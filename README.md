# Nimbus

Typed, context-injected, multi-cloud configuration manager for Go.

Nimbus loads cloud credentials from environment variables, validates them, builds
the SDK-native config object for each provider, and injects everything into a
`context.Context`. Downstream code retrieves what it needs by type — no globals,
no singletons, no plumbing through function signatures.

```
PROVIDERS=aws,gcp go run ./examples/multicloud
```

## Install

```sh
go get github.com/victorialuquet/nimbus
```

Requires Go 1.21+.

## What nimbus returns

Each built-in provider builds and exposes the **SDK-native config type** — not a
custom struct. You pass it directly to any service client without extra wiring:

| Provider | Type returned by `Retrieve` | Usage |
|----------|-----------------------------|-------|
| `aws`    | `aws.Config`                | `s3.NewFromConfig(cfg)`, `dynamodb.NewFromConfig(cfg)`, … |
| `gcp`    | `*auth.Credentials`         | `storage.NewClient(ctx, option.WithAuthCredentials(creds))`, … |
| `azure`  | `azcore.TokenCredential`    | `armresources.NewClient(subID, cred, nil)`, … |

## Usage

### 1. App environment config

```go
type AppConfig struct {
    Port  int    `env:"PORT,default=8080"    validate:"min=1,max=65535"`
    DBUrl string `env:"DATABASE_URL,required" validate:"url"`
}

ctx, err := nimbus.SetupEnv[AppConfig](ctx, env.WithDotenv())
cfg := nimbus.MustEnvFrom[AppConfig](ctx)
```

### 2. Cloud provider config

```sh
export PROVIDERS=aws,gcp
export AWS_REGION=us-east-1
export AWS_ACCESS_KEY_ID=...
export AWS_SECRET_ACCESS_KEY=...
export GCP_PROJECT_ID=my-project
# GOOGLE_APPLICATION_CREDENTIALS=/path/to/key.json  (outside GCP)
```

```go
ctx, err := nimbus.SetupProviders(ctx)

// aws.Config — ready for any AWS service client
awsCfg, err := nimbus.Retrieve[aws.Config](ctx)
s3Client := s3.NewFromConfig(awsCfg)

// *auth.Credentials — ready for any GCP client library
gcpCreds, err := nimbus.Retrieve[*auth.Credentials](ctx)
storageClient, err := storage.NewClient(ctx, option.WithAuthCredentials(gcpCreds))
```

### 3. Multi-region / multi-account

Each instance reads from its own **prefixed** env vars. No fallback to the base
vars — isolation is strict to avoid accidentally loading the wrong credentials.

```sh
export PROVIDERS=aws-us,aws-eu
export AWS_US_REGION=us-east-1
export AWS_US_ACCESS_KEY_ID=...
export AWS_US_SECRET_ACCESS_KEY=...
export AWS_EU_REGION=eu-west-1
export AWS_EU_ACCESS_KEY_ID=...
export AWS_EU_SECRET_ACCESS_KEY=...
```

```go
ctx, err := nimbus.SetupProviders(ctx,
    provider.WithProviders(
        aws.NewProvider("aws-us", "AWS_US_"),
        aws.NewProvider("aws-eu", "AWS_EU_"),
    ),
)

usCfg, _ := nimbus.RetrieveByName[aws.Config](ctx, "aws-us")
euCfg, _ := nimbus.RetrieveByName[aws.Config](ctx, "aws-eu")

usS3 := s3.NewFromConfig(usCfg)
euS3 := s3.NewFromConfig(euCfg)
```

The same prefix mechanism works for GCP (`GCP_PROD_`, `GCP_STAGING_`) and Azure
(`AZURE_PROD_`, `AZURE_STAGING_`).

### 4. Custom provider

Implement `provider.Provider` and pass it via `WithProviders`:

```go
type VaultProvider struct{ cfg VaultConfig }

func (p *VaultProvider) Name() string                   { return "vault" }
func (p *VaultProvider) Load(ctx context.Context) error { /* parse env, build client */ }
func (p *VaultProvider) Validate() error                { /* check required fields */ }
func (p *VaultProvider) Config() any                    { return &p.cfg }

ctx, err := nimbus.SetupProviders(ctx, provider.WithProviders(&VaultProvider{}))
vaultCfg, err := nimbus.Retrieve[*VaultConfig](ctx)
```

See [`examples/custom_provider/`](examples/custom_provider/) for a full example
with the optional `Observable` (Ping) interface.

## Built-in providers

### AWS

| Env var                | Required | Description                                  |
|------------------------|----------|----------------------------------------------|
| `AWS_REGION`           | yes      | Region (e.g. `us-east-1`)                   |
| `AWS_ACCESS_KEY_ID`    | no       | Static credential key                        |
| `AWS_SECRET_ACCESS_KEY`| no       | Static credential secret (required if key set)|
| `AWS_ROLE_ARN`         | no       | IAM role to assume                           |
| `AWS_PROFILE`          | no       | Named profile from `~/.aws/credentials`      |
| `AWS_ENDPOINT`         | no       | Custom endpoint (e.g. LocalStack)            |

If none of the optional auth fields are set, the AWS SDK default credential chain
is used (instance role, ECS task role, etc.).

### GCP

| Env var                          | Required | Description                        |
|----------------------------------|----------|------------------------------------|
| `GCP_PROJECT_ID`                 | yes      | Project identifier                 |
| `GOOGLE_APPLICATION_CREDENTIALS` | no       | Path to service account key or WIF config |
| `GCP_REGION`                     | no       | Default region (e.g. `us-central1`)|
| `GCP_ZONE`                       | no       | Default zone (e.g. `us-central1-a`)|

Credential resolution follows Application Default Credentials (ADC): service
account key → gcloud application-default → metadata server. When running outside
GCP (AWS, Azure, on-prem), point `GOOGLE_APPLICATION_CREDENTIALS` to a
[Workload Identity Federation](https://cloud.google.com/iam/docs/workload-identity-federation)
config file — no code changes needed.

### Azure

| Env var                  | Required | Description                                         |
|--------------------------|----------|-----------------------------------------------------|
| `AZURE_SUBSCRIPTION_ID`  | yes      | Subscription identifier                             |
| `AZURE_TENANT_ID`        | no       | AAD tenant (required for service principal)         |
| `AZURE_CLIENT_ID`        | no       | Service principal or managed identity client ID     |
| `AZURE_CLIENT_SECRET`    | no       | Service principal secret (requires tenant + client) |
| `AZURE_RESOURCE_GROUP`   | no       | Default resource group                              |
| `AZURE_LOCATION`         | no       | Default region (e.g. `eastus`)                      |

Credential resolution order:
1. `CLIENT_ID` + `CLIENT_SECRET` + `TENANT_ID` → `ClientSecretCredential` (service principal)
2. `CLIENT_ID` only → `ManagedIdentityCredential` (user-assigned)
3. None → `DefaultAzureCredential` (system-assigned identity, Azure CLI, env vars)

## Optional provider interfaces

| Interface     | Method                 | Purpose                            |
|---------------|------------------------|------------------------------------|
| `Refreshable` | `Refresh(ctx) error`   | Rotate credentials at runtime      |
| `Observable`  | `Ping(ctx) error`      | Connectivity health check          |
| `Dependent`   | `DependsOn() []string` | Declare load-order dependencies    |

Enable `Ping` checks at startup with `provider.WithPing()`.

## Dependencies

| Package                              | Purpose                         |
|--------------------------------------|---------------------------------|
| `aws-sdk-go-v2/config`               | AWS SDK config construction     |
| `cloud.google.com/go/auth`           | GCP credential resolution (ADC) |
| `azure-sdk-for-go/sdk/azidentity`    | Azure credential construction   |
| `go-playground/validator/v10`        | Struct validation tags          |
| `joho/godotenv`                      | `.env` file loading             |

The env parser (`internal/envparse`) has zero external dependencies.

## Examples

| Example | What it shows |
|---------|---------------|
| [`examples/basic/`](examples/basic/) | App env config with `.env` file |
| [`examples/multicloud/`](examples/multicloud/) | AWS + GCP providers, retrieve SDK configs |
| [`examples/multicloud/multiregion/`](examples/multicloud/multiregion/) | Two AWS instances with env var prefixes |
| [`examples/custom_provider/`](examples/custom_provider/) | Custom Vault provider with Ping support |
