package datasources_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccPricingDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
provider "thundercompute" {}

data "thundercompute_pricing" "current" {}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.thundercompute_pricing.current", "pricing.%"),
					// Canonical GPU/count spec key
					resource.TestCheckResourceAttrSet("data.thundercompute_pricing.current", "pricing.a6000_x1"),
					// Legacy bare GPU and _native aliases
					resource.TestCheckResourceAttrSet("data.thundercompute_pricing.current", "pricing.h100"),
					resource.TestCheckResourceAttrSet("data.thundercompute_pricing.current", "pricing.h100_native"),
					// Per-unit component rates
					resource.TestCheckResourceAttrSet("data.thundercompute_pricing.current", "pricing.additional_vcpus"),
					resource.TestCheckResourceAttrSet("data.thundercompute_pricing.current", "pricing.disk_gb"),
					resource.TestCheckResourceAttrSet("data.thundercompute_pricing.current", "pricing.snapshot_gb"),
				),
			},
		},
	})
}
