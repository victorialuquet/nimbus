// Package main demonstrates loading AWS and GCP providers and using the
// resulting SDK-ready credentials to construct service clients.
//
// nimbus resolves credentials once at startup. The returned types are ready
// to pass directly to any service client — no extra wiring required.
//
// # Running this example
//
//	export PROVIDERS=aws,gcp
//
//	# AWS — static credentials or leave unset to use the default chain
//	export AWS_REGION=us-east-1
//	export AWS_ACCESS_KEY_ID=...
//	export AWS_SECRET_ACCESS_KEY=...
//
//	# GCP — service account key, WIF config, or leave unset for ADC
//	export GCP_PROJECT_ID=my-project
//	export GOOGLE_APPLICATION_CREDENTIALS=/path/to/credentials.json
//
//	go run .
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/victorialuquet/nimbus"
	"github.com/victorialuquet/nimbus/provider"

	googleauth "cloud.google.com/go/auth"
	"github.com/aws/aws-sdk-go-v2/aws"
)

func main() {
	ctx := context.Background()

	ctx, err := nimbus.SetupProviders(ctx,
		provider.WithObserver(
			func(name string) { fmt.Printf("provider loaded: %s\n", name) },
			func(name string, err error) { fmt.Printf("provider error: %s — %v\n", name, err) },
		),
	)
	if err != nil {
		log.Fatalf("providers: %v", err)
	}

	// ── AWS ──────────────────────────────────────────────────────────────────
	// aws.Config is the single type accepted by every AWS service client.
	// Credentials, region, and endpoint are already baked in — pass it
	// directly to any constructor:
	//
	//   import "github.com/aws/aws-sdk-go-v2/service/s3"
	//   s3Client := s3.NewFromConfig(awsCfg)
	//
	//   import "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	//   dynClient := dynamodb.NewFromConfig(awsCfg)

	awsCfg, err := nimbus.Retrieve[aws.Config](ctx)
	if err != nil {
		log.Fatalf("aws: %v", err)
	}
	fmt.Printf("AWS region: %s\n", awsCfg.Region)

	// ── GCP ──────────────────────────────────────────────────────────────────
	// *auth.Credentials covers ADC, service account keys, and Workload
	// Identity Federation (for running outside GCP, e.g. on AWS or Azure).
	// Pass it via option.WithAuthCredentials to any GCP client library:
	//
	//   import "google.golang.org/api/option"
	//   import "cloud.google.com/go/storage"
	//   storageClient, _ := storage.NewClient(ctx, option.WithAuthCredentials(gcpCreds))
	//
	//   import "cloud.google.com/go/bigquery"
	//   bqClient, _ := bigquery.NewClient(ctx, projectID, option.WithAuthCredentials(gcpCreds))

	gcpCreds, err := nimbus.Retrieve[*googleauth.Credentials](ctx)
	if err != nil {
		log.Fatalf("gcp: %v", err)
	}
	fmt.Printf("GCP credentials ready (type: %T)\n", gcpCreds)
}
