// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package omni

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	omnires "github.com/siderolabs/omni/client/pkg/omni/resources/omni"
	"github.com/stretchr/testify/assert"
)

func TestMachineInstallDiskApplySpec(t *testing.T) {
	r := &machineInstallDiskResource{}

	for _, tc := range []struct {
		name             string
		plan             machineInstallDiskResourceModel
		expectedDisk     string
		expectedSelector string
	}{
		{
			name: "by dev path",
			plan: machineInstallDiskResourceModel{
				Disk:         types.StringValue("/dev/sda"),
				DiskSelector: types.StringNull(),
			},
			expectedDisk: "/dev/sda",
		},
		{
			name: "by selector",
			plan: machineInstallDiskResourceModel{
				Disk:         types.StringNull(),
				DiskSelector: types.StringValue("disk.transport == 'nvme'"),
			},
			expectedSelector: "disk.transport == 'nvme'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := omnires.NewMachineInstallDiskConfig("392102d6-6954-4f9b-a65d-896af85bca09")

			r.applySpec(tc.plan, config)

			assert.Equal(t, tc.expectedDisk, config.TypedSpec().Value.GetDisk())
			assert.Equal(t, tc.expectedSelector, config.TypedSpec().Value.GetDiskSelector())
		})
	}
}

// TestMachineInstallDiskApplySpecClearsPreviousSelection asserts that switching from one selection
// form to the other clears the form that is no longer configured: Omni stores exactly one of them.
func TestMachineInstallDiskApplySpecClearsPreviousSelection(t *testing.T) {
	r := &machineInstallDiskResource{}

	config := omnires.NewMachineInstallDiskConfig("392102d6-6954-4f9b-a65d-896af85bca09")
	config.TypedSpec().Value.Disk = "/dev/sda"

	r.applySpec(machineInstallDiskResourceModel{
		Disk:         types.StringNull(),
		DiskSelector: types.StringValue("system_disk"),
	}, config)

	assert.Empty(t, config.TypedSpec().Value.GetDisk())
	assert.Equal(t, "system_disk", config.TypedSpec().Value.GetDiskSelector())
}

func TestMachineInstallDiskSpecToModel(t *testing.T) {
	r := &machineInstallDiskResource{}

	config := omnires.NewMachineInstallDiskConfig("392102d6-6954-4f9b-a65d-896af85bca09")
	config.TypedSpec().Value.Disk = "/dev/sda"

	var model machineInstallDiskResourceModel

	r.specToModel(config, &model)

	assert.Equal(t, "392102d6-6954-4f9b-a65d-896af85bca09", model.MachineID.ValueString())
	assert.Equal(t, "/dev/sda", model.Disk.ValueString())
	// The unused selection form is reported as an empty string and must map back to null, otherwise
	// it diffs forever against a configuration that leaves it out.
	assert.True(t, model.DiskSelector.IsNull())
}
