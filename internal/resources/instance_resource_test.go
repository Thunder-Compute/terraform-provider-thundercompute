package resources_test

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"terraform-provider-thundercompute/internal/client"
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
	if os.Getenv("THUNDER_ALLOW_BILLABLE_TESTS") != "1" {
		t.Fatal("THUNDER_ALLOW_BILLABLE_TESTS=1 must be set to acknowledge billable/destructive acceptance tests")
	}
}

var testAccResourceSuffix = fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())

func testAccResourceName(prefix string) string {
	return prefix + "-" + testAccResourceSuffix
}

func testAccClient() *client.Client {
	return client.NewClient("", os.Getenv("TNR_API_TOKEN"), "test")
}

func checkInstanceDestroyed(s *terraform.State) error {
	c := testAccClient()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "thundercompute_instance" {
			continue
		}
		id := rs.Primary.Attributes["id"]
		if id == "" {
			continue
		}
		idx, item, err := c.GetInstanceByUUID(context.Background(), id)
		if err != nil {
			return fmt.Errorf("error checking instance %s destruction: %w", id, err)
		}
		if item != nil {
			if idx != "" {
				_ = c.DeleteInstance(context.Background(), idx)
			}
			return fmt.Errorf("instance %s still exists after destroy", id)
		}
	}
	return nil
}

func TestAccInstanceResource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             checkInstanceDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_basic(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("thundercompute_instance.test", "id"),
					resource.TestCheckResourceAttrSet("thundercompute_instance.test", "ip"),
					resource.TestCheckResourceAttrSet("thundercompute_instance.test", "status"),
					resource.TestCheckResourceAttr("thundercompute_instance.test", "gpu_type", "A6000"),
					resource.TestCheckResourceAttr("thundercompute_instance.test", "mode", "prototyping"),
					resource.TestCheckResourceAttr("thundercompute_instance.test", "template", "base"),
					resource.TestCheckResourceAttr("thundercompute_instance.test", "cpu_cores", "4"),
					resource.TestCheckResourceAttr("thundercompute_instance.test", "disk_size_gb", "100"),
					resource.TestCheckResourceAttr("thundercompute_instance.test", "num_gpus", "1"),
					resource.TestCheckResourceAttr("thundercompute_instance.test", "allow_snapshot_modify", "false"),
				),
			},
		},
	})
}

func TestAccInstanceResource_update(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             checkInstanceDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_withSnapshotModify(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("thundercompute_instance.test", "cpu_cores", "4"),
					resource.TestCheckResourceAttr("thundercompute_instance.test", "gpu_type", "A6000"),
				),
			},
			{
				Config: testAccInstanceConfig_updated(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("thundercompute_instance.test", "cpu_cores", "8"),
					resource.TestCheckResourceAttr("thundercompute_instance.test", "gpu_type", "A6000"),
					resource.TestCheckResourceAttr("thundercompute_instance.test", "template", "base"),
					resource.TestCheckResourceAttrSet("thundercompute_instance.test", "id"),
					resource.TestCheckResourceAttrSet("thundercompute_instance.test", "ip"),
				),
			},
		},
	})
}

func TestAccInstanceResource_recreate(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             checkInstanceDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_basic(),
				Check:  resource.TestCheckResourceAttr("thundercompute_instance.test", "template", "base"),
			},
			{
				Config: testAccInstanceConfig_differentTemplate(),
				Check:  resource.TestCheckResourceAttr("thundercompute_instance.test", "template", "cuda12-9"),
			},
		},
	})
}

func TestAccInstanceResource_import(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             checkInstanceDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_basic(),
			},
			{
				ResourceName:            "thundercompute_instance.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"public_key", "generated_key", "allow_snapshot_modify", "timeouts"},
			},
		},
	})
}

func TestAccInstanceResource_disappears(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             checkInstanceDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccInstanceConfig_basic(),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccDeleteInstanceOutOfBand("thundercompute_instance.test"),
				),
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func testAccDeleteInstanceOutOfBand(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %s not found", resourceName)
		}
		c := testAccClient()
		id := rs.Primary.Attributes["id"]
		idx, item, err := c.GetInstanceByUUID(context.Background(), id)
		if err != nil {
			return err
		}
		if item == nil {
			return fmt.Errorf("instance %s already gone", id)
		}
		return c.DeleteInstance(context.Background(), idx)
	}
}

func TestInstanceResourcePlanValidation(t *testing.T) {
	tests := []struct {
		name        string
		numGPUs     int
		attributes  string
		expectError string
	}{
		{name: "mode omitted", attributes: ""},
		{name: "matching mode", attributes: `mode = "prototyping"`},
		{name: "conflicting mode", attributes: `mode = "production"`, expectError: "Mode conflicts with GPU count"},
		{name: "invalid GPU count", numGPUs: 3, expectError: "1.*2.*4.*8"},
		{name: "SSH port rejected", attributes: "http_ports = [22]", expectError: "22"},
		{name: "out of range port rejected", attributes: "http_ports = [65536]", expectError: "65535"},
		{name: "invalid public key rejected", attributes: `public_key = "not-a-public-key"`, expectError: "OpenSSH public key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			numGPUs := tt.numGPUs
			if numGPUs == 0 {
				numGPUs = 1
			}
			step := resource.TestStep{
				Config:             instancePlanValidationConfig(numGPUs, tt.attributes),
				PlanOnly:           true,
				ExpectNonEmptyPlan: tt.expectError == "",
			}
			if tt.expectError != "" {
				step.ExpectError = regexp.MustCompile(tt.expectError)
			}
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
				Steps:                    []resource.TestStep{step},
			})
		})
	}
}

func instancePlanValidationConfig(numGPUs int, attributes string) string {
	return fmt.Sprintf(`
provider "thundercompute" {
  api_token = "unit-test-token"
}

resource "thundercompute_instance" "test" {
  gpu_type     = "A6000"
  template     = "base"
  cpu_cores    = 4
  disk_size_gb = 100
  num_gpus     = %d
  %s
}
`, numGPUs, attributes)
}

// All configs use A6000 prototyping ($0.35/hr) -- cheapest available
func testAccInstanceConfig_basic() string {
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
`
}

func testAccInstanceConfig_withSnapshotModify() string {
	return `
provider "thundercompute" {}

resource "thundercompute_instance" "test" {
  gpu_type              = "A6000"
  mode                  = "prototyping"
  template              = "base"
  cpu_cores             = 4
  disk_size_gb          = 100
  num_gpus              = 1
  allow_snapshot_modify = true
}
`
}

func testAccInstanceConfig_updated() string {
	return `
provider "thundercompute" {}

resource "thundercompute_instance" "test" {
  gpu_type              = "A6000"
  mode                  = "prototyping"
  template              = "base"
  cpu_cores             = 8
  disk_size_gb          = 100
  num_gpus              = 1
  http_ports            = [8888]
  allow_snapshot_modify = true
}
`
}

func testAccInstanceConfig_differentTemplate() string {
	return `
provider "thundercompute" {}

resource "thundercompute_instance" "test" {
  gpu_type     = "A6000"
  mode         = "prototyping"
  template     = "cuda12-9"
  cpu_cores    = 4
  disk_size_gb = 100
  num_gpus     = 1
}
`
}
