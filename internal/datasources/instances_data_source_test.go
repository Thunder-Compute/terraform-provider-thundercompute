package datasources_test

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"terraform-provider-thundercompute/internal/provider"
)

func testAccProtoV6ProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"thundercompute": providerserver.NewProtocol6WithError(provider.New("test")()),
	}
}

func testAccPreCheck(t *testing.T) {
	if v := os.Getenv("TNR_API_TOKEN"); v == "" {
		t.Fatal("TNR_API_TOKEN must be set for acceptance tests")
	}
}

func TestAccInstancesDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
provider "thundercompute" {}

data "thundercompute_instances" "all" {}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.thundercompute_instances.all", "instances.#"),
				),
			},
		},
	})
}
