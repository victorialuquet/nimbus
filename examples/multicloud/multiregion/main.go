// Package main demonstrates registering two AWS provider instances for
// multi-region deployments using named providers with env var prefixes.
//
// Each provider reads from its own prefixed env vars, falling back to the
// unprefixed name if the prefixed one is absent. This allows per-region
// credentials, endpoints, and regions to be configured independently.
//
// # Running this example
//
//	export PROVIDERS=aws-us,aws-eu
//
//	export AWS_US_REGION=us-east-1
//	export AWS_US_ACCESS_KEY_ID=AKIA...
//	export AWS_US_SECRET_ACCESS_KEY=...
//
//	export AWS_EU_REGION=eu-west-1
//	export AWS_EU_ACCESS_KEY_ID=AKIA...
//	export AWS_EU_SECRET_ACCESS_KEY=...
//
//	go run .
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/victorialuquet/nimbus"
	"github.com/victorialuquet/nimbus/provider"
	nimbusaws "github.com/victorialuquet/nimbus/providers/aws"
)

func main() {
	ctx := context.Background()

	ctx, err := nimbus.SetupProviders(ctx,
		provider.WithProviders(
			nimbusaws.NewProvider("aws-us", "AWS_US_"),
			nimbusaws.NewProvider("aws-eu", "AWS_EU_"),
		),
	)
	if err != nil {
		log.Fatalf("providers: %v", err)
	}

	// RetrieveByName returns the aws.Config for that specific named provider.
	// Each config has its own region and credentials baked in — pass directly
	// to any AWS service client:
	//
	//   import "github.com/aws/aws-sdk-go-v2/service/s3"
	//   usS3 := s3.NewFromConfig(usCfg)
	//   euS3 := s3.NewFromConfig(euCfg)

	usCfg, err := nimbus.RetrieveByName[aws.Config](ctx, "aws-us")
	if err != nil {
		log.Fatal(err)
	}
	euCfg, err := nimbus.RetrieveByName[aws.Config](ctx, "aws-eu")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("US region: %s\n", usCfg.Region)
	fmt.Printf("EU region: %s\n", euCfg.Region)
}
