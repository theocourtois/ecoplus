package provider

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func serverConfig(name, serverType, image, extra string) string {
	return fmt.Sprintf(`
resource "ecoplus_server" "s" {
  name        = %q
  server_type = %q
  location    = "nbg1"
  image       = %q
  %s
}
`, name, serverType, image, extra)
}

type captured struct{ id, lease string }

func capture(dst *captured) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs := s.RootModule().Resources["ecoplus_server.s"]
		if rs == nil {
			return fmt.Errorf("resource not in state")
		}
		dst.id, dst.lease = rs.Primary.Attributes["id"], rs.Primary.Attributes["lease"]
		return nil
	}
}

func sameAs(prev *captured) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs := s.RootModule().Resources["ecoplus_server.s"]
		if rs.Primary.Attributes["id"] != prev.id || rs.Primary.Attributes["lease"] != prev.lease {
			return fmt.Errorf("server was replaced: %v vs %+v", rs.Primary.Attributes, *prev)
		}
		return nil
	}
}

func differentFrom(prev *captured) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs := s.RootModule().Resources["ecoplus_server.s"]
		if rs.Primary.Attributes["id"] == prev.id {
			return fmt.Errorf("server was not replaced")
		}
		return nil
	}
}

func action(a plancheck.ResourceActionType) resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("ecoplus_server.s", a)}}
}

func TestCreateUpdateReplace(t *testing.T) {
	t.Parallel()
	c := newController(t)
	var first, second captured
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: c.factories(),
		Steps: []resource.TestStep{
			{
				Config: c.config(serverConfig("web", "cx22", "ubuntu", "")),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("ecoplus_server.s", "name", "web"),
					resource.TestCheckResourceAttr("ecoplus_server.s", "image", "ubuntu"),
					resource.TestCheckResourceAttr("ecoplus_server.s", "reused", "false"),
					resource.TestCheckResourceAttrSet("ecoplus_server.s", "lease"),
					resource.TestCheckResourceAttrSet("ecoplus_server.s", "ipv4_address"),
					resource.TestCheckResourceAttrSet("ecoplus_server.s", "ipv6_address"),
					capture(&first),
				),
			},
			{
				Config:           c.config(serverConfig("web", "cx22", "debian", "")),
				ConfigPlanChecks: action(plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("ecoplus_server.s", "image", "debian"), sameAs(&first)),
			},
			{
				Config:           c.config(serverConfig("renamed", "cx22", "debian", "")),
				ConfigPlanChecks: action(plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("ecoplus_server.s", "name", "renamed"), sameAs(&first)),
			},
			{
				Config:           c.config(serverConfig("renamed", "cx22", "debian", `labels = { team = "infra" }`)),
				ConfigPlanChecks: action(plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("ecoplus_server.s", "labels.team", "infra"), sameAs(&first)),
			},
			{
				Config:           c.config(serverConfig("renamed", "cx32", "debian", `labels = { team = "infra" }`)),
				ConfigPlanChecks: action(plancheck.ResourceActionDestroyBeforeCreate),
				Check:            resource.ComposeTestCheckFunc(differentFrom(&first), capture(&second)),
			},
		},
	})
}

func TestSSHKeyOrderProducesNoDiff(t *testing.T) {
	t.Parallel()
	c := newController(t)
	c.cloud.AddSSHKey("a")
	c.cloud.AddSSHKey("b")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: c.factories(),
		Steps: []resource.TestStep{
			{Config: c.config(serverConfig("web", "cx22", "ubuntu", `ssh_keys = ["a", "b"]`))},
			{Config: c.config(serverConfig("web", "cx22", "ubuntu", `ssh_keys = ["b", "a"]`)), PlanOnly: true},
		},
	})
}

func TestReadNotFoundPlansRecreate(t *testing.T) {
	t.Parallel()
	c := newController(t)
	cfg := c.config(serverConfig("web", "cx22", "ubuntu", ""))
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: c.factories(),
		Steps: []resource.TestStep{
			{Config: cfg},
			{PreConfig: c.releaseOutOfBand, Config: cfg, PlanOnly: true, ExpectNonEmptyPlan: true},
		},
	})
}

func TestReadUnavailableKeepsResourceInState(t *testing.T) {
	t.Parallel()
	c := newController(t)
	cfg := c.config(serverConfig("web", "cx22", "ubuntu", ""))
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: c.factories(),
		Steps: []resource.TestStep{
			{Config: cfg},
			{PreConfig: func() { c.failGet.Store(true) }, Config: cfg, PlanOnly: true, ExpectError: regexp.MustCompile("Reading server failed")},
			{PreConfig: func() { c.failGet.Store(false) }, Config: cfg, PlanOnly: true},
		},
	})
}

func TestImport(t *testing.T) {
	t.Parallel()
	c := newController(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: c.factories(),
		Steps: []resource.TestStep{
			{Config: c.config(serverConfig("web", "cx22", "ubuntu", ""))},
			{
				ResourceName: "ecoplus_server.s", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					a := s.RootModule().Resources["ecoplus_server.s"].Primary.Attributes
					return a["id"] + "/" + a["lease"], nil
				},
			},
			{ResourceName: "ecoplus_server.s", ImportState: true, ImportStateId: "garbage", ExpectError: regexp.MustCompile("Invalid import ID")},
		},
	})
}

func TestInvalidConfiguration(t *testing.T) {
	t.Parallel()
	c := newController(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: c.factories(),
		Steps: []resource.TestStep{
			{Config: c.config(serverConfig("web", "cx22", "ubuntu", `labels = { "ecoplus/x" = "y" }`)), PlanOnly: true, ExpectError: regexp.MustCompile("Reserved label key")},
			{Config: c.config(serverConfig("Bad_Name", "cx22", "ubuntu", "")), PlanOnly: true, ExpectError: regexp.MustCompile("RFC 1123")},
		},
	})
}

func setEnv(t *testing.T, values map[string]string) func() {
	return func() {
		for k, v := range values {
			prev, had := os.LookupEnv(k)
			if err := os.Setenv(k, v); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if had {
					_ = os.Setenv(k, prev)
				} else {
					_ = os.Unsetenv(k)
				}
			})
		}
	}
}

func TestProviderConfigurationEnvironment(t *testing.T) {
	t.Parallel()
	c := newController(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: c.factories(),
		Steps: []resource.TestStep{
			{
				PreConfig:   setEnv(t, map[string]string{"ECOPLUS_ENDPOINT": "", "ECOPLUS_TOKEN": ""}),
				Config:      `provider "ecoplus" {}` + serverConfig("web", "cx22", "ubuntu", ""),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("Missing (endpoint|token)"),
			},
			{
				PreConfig: setEnv(t, map[string]string{"ECOPLUS_ENDPOINT": c.url, "ECOPLUS_TOKEN": testToken}),
				Config:    serverConfig("web", "cx22", "ubuntu", ""),
			},
		},
	})
}
