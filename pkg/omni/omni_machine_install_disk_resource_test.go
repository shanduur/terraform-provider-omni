// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package omni_test

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/cosi-project/runtime/pkg/safe"
	cosistate "github.com/cosi-project/runtime/pkg/state"
	"github.com/google/uuid"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	omnires "github.com/siderolabs/omni/client/pkg/omni/resources/omni"

	"github.com/siderolabs/terraform-provider-omni/pkg/omni"
)

// TestAccOmniMachineInstallDiskResource exercises the full lifecycle of an install disk selection:
// create by dev path, switch to a disk selector in place, import, and destroy.
//
// The selection is a machine-scoped resource that Omni accepts independently of the machine having
// joined, so the test uses a random UUID rather than a real machine. Whether the selection actually
// resolves to a disk is a machine-side concern covered by the QEMU end-to-end suite.
func TestAccOmniMachineInstallDiskResource(t *testing.T) {
	machineID := uuid.NewString()

	tfresource.ParallelTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: omni.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckMachineInstallDiskDestroy,
		Steps: []tfresource.TestStep{
			{ // create with a dev path
				Config: testAccMachineInstallDiskConfig(machineID, `disk = "/dev/sda"`),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr("omni_machine_install_disk.test", "machine_id", machineID),
					tfresource.TestCheckResourceAttr("omni_machine_install_disk.test", "disk", "/dev/sda"),
					tfresource.TestCheckNoResourceAttr("omni_machine_install_disk.test", "disk_selector"),
					testAccCheckMachineInstallDisk(machineID, "/dev/sda", ""),
				),
			},
			{ // import by machine UUID
				ResourceName:                         "omni_machine_install_disk.test",
				ImportState:                          true,
				ImportStateId:                        machineID,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "machine_id",
			},
			{ // switching to a selector is an in-place update that clears the dev path
				Config: testAccMachineInstallDiskConfig(machineID, `disk_selector = "disk.transport == 'nvme'"`),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr("omni_machine_install_disk.test", "disk_selector", "disk.transport == 'nvme'"),
					tfresource.TestCheckNoResourceAttr("omni_machine_install_disk.test", "disk"),
					testAccCheckMachineInstallDisk(machineID, "", "disk.transport == 'nvme'"),
				),
			},
		},
	})
}

// TestAccOmniMachineInstallDiskSelectionGuard asserts that the mutual exclusivity of the two
// selection forms is enforced: Omni stores exactly one of them, so neither an empty nor an
// ambiguous selection may reach it. These cases stand alone (no CheckDestroy) because the config
// never plans successfully, so nothing is created.
func TestAccOmniMachineInstallDiskSelectionGuard(t *testing.T) {
	for _, tc := range []struct {
		expectError *regexp.Regexp
		name        string
		selection   string
	}{
		{
			name:        "both selection forms",
			selection:   "disk = \"/dev/sda\"\n  disk_selector = \"system_disk\"",
			expectError: regexp.MustCompile("Invalid Attribute Combination"),
		},
		{
			// ExactlyOneOf reports an empty selection under a different summary than an ambiguous
			// one, so the two cases cannot share a pattern.
			name:        "no selection form",
			selection:   "",
			expectError: regexp.MustCompile("Missing Attribute Configuration"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tfresource.ParallelTest(t, tfresource.TestCase{
				ProtoV6ProviderFactories: omni.TestAccProtoV6ProviderFactories,
				Steps: []tfresource.TestStep{
					{
						Config:      testAccMachineInstallDiskConfig(uuid.NewString(), tc.selection),
						ExpectError: tc.expectError,
					},
				},
			})
		})
	}
}

func testAccMachineInstallDiskConfig(machineID, selection string) string {
	return fmt.Sprintf(`
provider "omni" {
  insecure_skip_tls_verify = true
}

resource "omni_machine_install_disk" "test" {
  machine_id = %q
  %s
}
`, machineID, selection)
}

// testAccCheckMachineInstallDisk asserts, via the live Omni API, that the selection was stored with
// exactly the expected form.
func testAccCheckMachineInstallDisk(machineID, disk, diskSelector string) tfresource.TestCheckFunc {
	return func(*terraform.State) error {
		client, err := newTestClient()
		if err != nil {
			return err
		}
		defer client.Close() //nolint:errcheck

		config, err := safe.ReaderGetByID[*omnires.MachineInstallDiskConfig](context.Background(), client.Omni().State(), machineID)
		if err != nil {
			return fmt.Errorf("failed to read install disk configuration %q: %w", machineID, err)
		}

		if got := config.TypedSpec().Value.GetDisk(); got != disk {
			return fmt.Errorf("unexpected disk for %q: got %q, want %q", machineID, got, disk)
		}

		if got := config.TypedSpec().Value.GetDiskSelector(); got != diskSelector {
			return fmt.Errorf("unexpected disk selector for %q: got %q, want %q", machineID, got, diskSelector)
		}

		return nil
	}
}

// testAccCheckMachineInstallDiskDestroy asserts, via the live Omni API, that every managed
// selection is gone.
func testAccCheckMachineInstallDiskDestroy(s *terraform.State) error {
	client, err := newTestClient()
	if err != nil {
		return err
	}
	defer client.Close() //nolint:errcheck

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "omni_machine_install_disk" {
			continue
		}

		machineID := rs.Primary.Attributes["machine_id"]

		_, err = safe.ReaderGetByID[*omnires.MachineInstallDiskConfig](context.Background(), client.Omni().State(), machineID)
		if err == nil {
			return fmt.Errorf("install disk configuration %q still exists", machineID)
		}

		if !cosistate.IsNotFoundError(err) {
			return fmt.Errorf("failed to check install disk configuration %q: %w", machineID, err)
		}
	}

	return nil
}
