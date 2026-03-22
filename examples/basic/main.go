// Package main demonstrates the basic usage of cloudcfg for loading
// application environment variables into a typed struct.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/victorialuquet/nimbus"
	"github.com/victorialuquet/nimbus/env"
)

// AppConfig holds all environment-driven configuration for this service.
type AppConfig struct {
	Port    int    `env:"PORT,default=8080"     validate:"min=1,max=65535"`
	DBUrl   string `env:"DATABASE_URL,required"  validate:"url"`
	Debug   bool   `env:"DEBUG,default=false"`
	AppName string `env:"APP_NAME,default=myapp"`
}

func main() {
	ctx := context.Background()

	// Load .env for local development (no-op if file is absent).
	ctx, err := nimbus.SetupEnv[AppConfig](ctx, env.WithDotenv())
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	cfg := nimbus.MustEnvFrom[AppConfig](ctx)
	fmt.Printf("Starting %s on :%d (debug=%v)\n", cfg.AppName, cfg.Port, cfg.Debug)
}
