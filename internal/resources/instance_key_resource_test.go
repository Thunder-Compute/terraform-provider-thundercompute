package resources_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccInstanceKeyResource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             checkInstanceDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceKeyConfig_basic(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("thundercompute_instance_key.test", "id"),
					resource.TestCheckResourceAttrSet("thundercompute_instance_key.test", "instance_id"),
					resource.TestCheckResourceAttr("thundercompute_instance_key.test", "public_key",
						"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIB1lnJOg4gHI9wg++M9T2SqaDMb8dw7ClZcKSAin/Pav tf-acc-instance-key@terraform"),
				),
			},
		},
	})
}

func TestAccInstanceKeyResource_disappearsWithInstance(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             checkInstanceDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceKeyConfig_basic(),
				Check:  testAccDeleteInstanceOutOfBand("thundercompute_instance.test"),
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestAccInstanceKeyResource_multipleKeys(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             checkInstanceDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceKeyConfig_multiple(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("thundercompute_instance_key.key1", "id"),
					resource.TestCheckResourceAttrSet("thundercompute_instance_key.key2", "id"),
				),
			},
		},
	})
}

func testAccInstanceKeyConfig_basic() string {
	return `
provider "thundercompute" {}

resource "thundercompute_instance" "test" {
  gpu_type     = "A6000"
  mode         = "prototyping"
  template     = "base"
  cpu_cores    = 4
  disk_size_gb = 100
  num_gpus     = 1
}

resource "thundercompute_instance_key" "test" {
  instance_id = thundercompute_instance.test.id
  public_key  = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIB1lnJOg4gHI9wg++M9T2SqaDMb8dw7ClZcKSAin/Pav tf-acc-instance-key@terraform"
}
`
}

func testAccInstanceKeyConfig_multiple() string {
	return `
provider "thundercompute" {}

resource "thundercompute_instance" "test" {
  gpu_type     = "A6000"
  mode         = "prototyping"
  template     = "base"
  cpu_cores    = 4
  disk_size_gb = 100
  num_gpus     = 1
}

resource "thundercompute_instance_key" "key1" {
  instance_id = thundercompute_instance.test.id
  public_key  = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIB1lnJOg4gHI9wg++M9T2SqaDMb8dw7ClZcKSAin/Pav tf-acc-key1@terraform"
}

resource "thundercompute_instance_key" "key2" {
  instance_id = thundercompute_instance.test.id
  public_key  = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHkHmTR9YDMZ1rG8mTZT5EE2bJo1H7x8v8xpF2qK0PW tf-acc-key2@terraform"
}
`
}
