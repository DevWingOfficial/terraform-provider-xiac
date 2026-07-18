package main

import (
	"context"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/XiaCOrg/terraform-provider-xiac/internal/provider"
)

// main serves the terraform-provider-xiac plugin over the plugin protocol.
func main() {
	err := providerserver.Serve(context.Background(), provider.New, providerserver.ServeOpts{
		Address: "registry.terraform.io/XiaCOrg/xiac",
	})
	if err != nil {
		log.Fatal(err)
	}
}
