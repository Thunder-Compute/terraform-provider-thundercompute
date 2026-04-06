package resources_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func checkSSHKeyDestroyed(s *terraform.State) error {
	c := testAccClient()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "thundercompute_ssh_key" {
			continue
		}
		id := rs.Primary.ID
		if id == "" {
			continue
		}
		key, err := c.GetSSHKeyByID(context.Background(), id)
		if err != nil {
			return fmt.Errorf("error checking SSH key %s destruction: %w", id, err)
		}
		if key != nil {
			_ = c.DeleteSSHKey(context.Background(), id)
			return fmt.Errorf("SSH key %s still exists after destroy", id)
		}
	}
	return nil
}

func TestAccSSHKeyResource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             checkSSHKeyDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccSSHKeyConfig_basic(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("thundercompute_ssh_key.test", "id"),
					resource.TestCheckResourceAttrSet("thundercompute_ssh_key.test", "fingerprint"),
					resource.TestCheckResourceAttr("thundercompute_ssh_key.test", "name", "tf-test-key"),
				),
			},
		},
	})
}

func TestAccSSHKeyResource_recreate(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             checkSSHKeyDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccSSHKeyConfig_basic(),
				Check:  resource.TestCheckResourceAttr("thundercompute_ssh_key.test", "name", "tf-test-key"),
			},
			{
				Config: testAccSSHKeyConfig_renamed(),
				Check:  resource.TestCheckResourceAttr("thundercompute_ssh_key.test", "name", "tf-test-key-renamed"),
			},
		},
	})
}

func TestAccSSHKeyResource_import(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             checkSSHKeyDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccSSHKeyConfig_basic(),
			},
			{
				ResourceName:      "thundercompute_ssh_key.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccSSHKeyResource_disappears(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             checkSSHKeyDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccSSHKeyConfig_basic(),
				Check:  testAccDeleteSSHKeyOutOfBand("thundercompute_ssh_key.test"),
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func testAccDeleteSSHKeyOutOfBand(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %s not found", resourceName)
		}
		c := testAccClient()
		return c.DeleteSSHKey(context.Background(), rs.Primary.ID)
	}
}

func testAccSSHKeyConfig_basic() string {
	return `
provider "thundercompute" {}

resource "thundercompute_ssh_key" "test" {
  name       = "tf-test-key"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIB1lnJOg4gHI9wg++M9T2SqaDMb8dw7ClZcKSAin/Pav tf-test@terraform"
}
`
}

func testAccSSHKeyConfig_renamed() string {
	return `
provider "thundercompute" {}

resource "thundercompute_ssh_key" "test" {
  name       = "tf-test-key-renamed"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIB1lnJOg4gHI9wg++M9T2SqaDMb8dw7ClZcKSAin/Pav tf-test@terraform"
}
`
}
