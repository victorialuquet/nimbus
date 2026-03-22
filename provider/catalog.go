package provider

import (
	"github.com/victorialuquet/nimbus/providers/aws"
	"github.com/victorialuquet/nimbus/providers/azure"
	"github.com/victorialuquet/nimbus/providers/gcp"
)

// builtinCatalog returns the default set of recognised provider names.
// Entries can be overridden by custom providers passed via [WithProviders].
func builtinCatalog() map[string]Provider {
	return map[string]Provider{
		"aws":   &aws.Provider{},
		"gcp":   &gcp.Provider{},
		"azure": &azure.Provider{},
	}
}
