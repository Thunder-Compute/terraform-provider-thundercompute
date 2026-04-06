package resources_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func checkSnapshotDestroyed(s *terraform.State) error {
	c := testAccClient()
	for _, rs := range s.RootModule().Resources {
		if rs.Type == "thundercompute_snapshot" {
			snapID := rs.Primary.ID
			if snapID == "" {
				continue
			}
			snap, err := c.GetSnapshotByID(context.Background(), snapID)
			if err != nil {
				return fmt.Errorf("error checking snapshot %s destruction: %w", snapID, err)
			}
			if snap != nil {
				_ = c.DeleteSnapshot(context.Background(), snapID)
				return fmt.Errorf("snapshot %s still exists after destroy", snapID)
			}
		}
		if rs.Type == "thundercompute_instance" {
			id := rs.Primary.Attributes["id"]
			if id == "" {
				continue
			}
			idx, item, _ := c.GetInstanceByUUID(context.Background(), id)
			if item != nil {
				_ = c.DeleteInstance(context.Background(), idx)
				return fmt.Errorf("instance %s still exists after destroy", id)
			}
		}
	}
	return nil
}

func TestAccSnapshotResource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             checkSnapshotDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccSnapshotConfig_basic(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("thundercompute_snapshot.test", "id"),
					resource.TestCheckResourceAttrSet("thundercompute_snapshot.test", "status"),
					resource.TestCheckResourceAttrSet("thundercompute_snapshot.test", "created_at"),
					resource.TestCheckResourceAttr("thundercompute_snapshot.test", "name", "tf-test-snapshot"),
				),
			},
		},
	})
}

func TestAccSnapshotResource_import(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             checkSnapshotDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccSnapshotConfig_basic(),
			},
			{
				ResourceName:            "thundercompute_snapshot.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"instance_id", "timeouts"},
			},
		},
	})
}

func TestAccSnapshotResource_disappears(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             checkSnapshotDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccSnapshotConfig_basic(),
				Check:  testAccDeleteSnapshotOutOfBand("thundercompute_snapshot.test"),
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func testAccDeleteSnapshotOutOfBand(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %s not found", resourceName)
		}
		c := testAccClient()
		return c.DeleteSnapshot(context.Background(), rs.Primary.ID)
	}
}

func testAccSnapshotConfig_basic() string {
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

resource "thundercompute_snapshot" "test" {
  instance_id = thundercompute_instance.test.id
  name        = "tf-test-snapshot"
}
`
}
