package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"terraform-provider-thundercompute/internal/provider"
)

var version = "dev"

func main() {
	var debug bool
	var showVersion bool
	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.BoolVar(&showVersion, "version", false, "print the provider version and exit")
	flag.Parse()
	if showVersion {
		fmt.Println(version)
		return
	}

	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/Thunder-Compute/thundercompute",
		Debug:   debug,
	}

	err := providerserver.Serve(context.Background(), provider.New(version), opts)
	if err != nil {
		log.Fatal(err.Error())
	}
}
