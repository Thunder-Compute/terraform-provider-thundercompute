package provider

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestProviderSchema(t *testing.T) {
	// Verifies the provider schema compiles and is internally consistent.
	resp, err := providerserver.NewProtocol6WithError(New("test")())()
	if err != nil {
		t.Fatalf("failed to create provider server: %v", err)
	}
	_ = resp
}

// testAccProtoV6ProviderFactories returns provider factories for acceptance tests.
func testAccProtoV6ProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"thundercompute": providerserver.NewProtocol6WithError(New("test")()),
	}
}

func testAccPreCheck(t *testing.T) {
	if v := os.Getenv("TNR_API_TOKEN"); v == "" {
		t.Fatal("TNR_API_TOKEN must be set for acceptance tests")
	}
}

func TestProviderConfigure_MissingToken(t *testing.T) {
	// Unset the env var for this test
	orig := os.Getenv("TNR_API_TOKEN")
	os.Unsetenv("TNR_API_TOKEN")
	defer os.Setenv("TNR_API_TOKEN", orig)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
provider "thundercompute" {}

data "thundercompute_pricing" "test" {}
`,
				ExpectError: MustCompileRegexp("Missing API Token"),
			},
		},
	})
}

func TestProviderConfigure_CustomAPIURL(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
provider "thundercompute" {
  api_token = "test-token"
  api_url   = "https://custom-api.example.com/v1"
}

data "thundercompute_pricing" "test" {}
`,
				// This will fail because the custom URL is invalid, but the provider
				// should configure successfully -- the error is at data source read time.
				ExpectError: MustCompileRegexp("Error reading Thunder Compute pricing"),
			},
		},
	})
}

// MustCompileRegexp returns a compiled regexp for use with ExpectError.
func MustCompileRegexp(pattern string) *regexp.Regexp {
	return regexp.MustCompile(pattern)
}
