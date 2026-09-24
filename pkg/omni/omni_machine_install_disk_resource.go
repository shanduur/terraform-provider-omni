// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package omni

import (
	"context"

	"github.com/cosi-project/runtime/pkg/safe"
	cosistate "github.com/cosi-project/runtime/pkg/state"
	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/siderolabs/omni/client/pkg/omni/resources/omni"
)

// Ensure the resource satisfies the framework interfaces.
var (
	_ resource.Resource                     = (*machineInstallDiskResource)(nil)
	_ resource.ResourceWithConfigure        = (*machineInstallDiskResource)(nil)
	_ resource.ResourceWithImportState      = (*machineInstallDiskResource)(nil)
	_ resource.ResourceWithConfigValidators = (*machineInstallDiskResource)(nil)
)

// machineInstallDiskResourceModel maps the omni_machine_install_disk resource schema.
type machineInstallDiskResourceModel struct {
	MachineID    types.String `tfsdk:"machine_id"`
	Disk         types.String `tfsdk:"disk"`
	DiskSelector types.String `tfsdk:"disk_selector"`
}

// machineInstallDiskResource implements the omni_machine_install_disk resource.
type machineInstallDiskResource struct {
	data *providerData
}

// NewMachineInstallDiskResource returns a new omni_machine_install_disk resource.
func NewMachineInstallDiskResource() resource.Resource {
	return &machineInstallDiskResource{}
}

// Metadata implements resource.Resource.
func (r *machineInstallDiskResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_machine_install_disk"
}

// Schema implements resource.Resource.
func (r *machineInstallDiskResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the install disk selection of a single machine. Omni owns `machine.install.disk` in the " +
			"generated machine configuration and rejects config patches that override it, so this resource is the way to " +
			"pick the install disk. Select the disk either by its dev path (`disk`) or by a CEL expression evaluated " +
			"against the machine's disks (`disk_selector`); exactly one of the two must be set.",
		Attributes: map[string]schema.Attribute{
			"machine_id": schema.StringAttribute{
				Required:    true,
				Description: "The machine UUID the selection applies to. Immutable.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"disk": schema.StringAttribute{
				Optional:    true,
				Description: "The install disk dev path, e.g. `/dev/sda`. Conflicts with `disk_selector`.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"disk_selector": schema.StringAttribute{
				Optional: true,
				Description: "A boolean CEL expression selecting the install disk, evaluated against each of the machine's " +
					"disks in the Talos disk locator environment (e.g. `disk.transport == \"nvme\"`). Conflicts with `disk`. " +
					"The expression is validated by Omni when the selection is written.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
		},
	}
}

// ConfigValidators implements resource.ResourceWithConfigValidators. Omni stores exactly one of the
// two selection forms, so requiring exactly one here rejects both an empty and an ambiguous
// selection before apply.
func (r *machineInstallDiskResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(
			path.MatchRoot("disk"),
			path.MatchRoot("disk_selector"),
		),
	}
}

// Configure implements resource.ResourceWithConfigure.
func (r *machineInstallDiskResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerDataFromResource(req.ProviderData, &resp.Diagnostics)
}

// applySpec copies the plan onto the install disk configuration spec. Both fields are always
// written so that switching between the two selection forms clears the previous one.
func (r *machineInstallDiskResource) applySpec(plan machineInstallDiskResourceModel, config *omni.MachineInstallDiskConfig) {
	spec := config.TypedSpec().Value

	spec.Disk = plan.Disk.ValueString()
	spec.DiskSelector = plan.DiskSelector.ValueString()
}

// Create implements resource.Resource.
func (r *machineInstallDiskResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan machineInstallDiskResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	config := omni.NewMachineInstallDiskConfig(plan.MachineID.ValueString())

	r.applySpec(plan, config)

	if err := r.data.state.Create(ctx, config); err != nil {
		errToDiag(&resp.Diagnostics, "Failed to create Omni machine install disk configuration", err)

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read implements resource.Resource.
func (r *machineInstallDiskResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state machineInstallDiskResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	config, err := safe.ReaderGetByID[*omni.MachineInstallDiskConfig](ctx, r.data.state, state.MachineID.ValueString())
	if err != nil {
		if cosistate.IsNotFoundError(err) {
			resp.State.RemoveResource(ctx)

			return
		}

		errToDiag(&resp.Diagnostics, "Failed to read Omni machine install disk configuration", err)

		return
	}

	r.specToModel(config, &state)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update implements resource.Resource. The machine forces replacement; the selection itself is
// updated in place, including a switch between `disk` and `disk_selector`.
func (r *machineInstallDiskResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan machineInstallDiskResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	updateWithDiags(ctx, r.data.state, omni.NewMachineInstallDiskConfig(plan.MachineID.ValueString()).Metadata(), &resp.Diagnostics,
		"Failed to update Omni machine install disk configuration",
		func(config *omni.MachineInstallDiskConfig) {
			r.applySpec(plan, config)
		})

	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete implements resource.Resource. Dropping the selection hands the machine back to Omni's
// automatic install disk choice.
func (r *machineInstallDiskResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state machineInstallDiskResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	config := omni.NewMachineInstallDiskConfig(state.MachineID.ValueString())

	if err := r.data.state.TeardownAndDestroy(ctx, config.Metadata()); err != nil {
		if cosistate.IsNotFoundError(err) {
			return
		}

		errToDiag(&resp.Diagnostics, "Failed to destroy Omni machine install disk configuration", err)

		return
	}
}

// ImportState implements resource.ResourceWithImportState. Install disk configurations are imported
// by the machine UUID.
func (r *machineInstallDiskResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("machine_id"), req, resp)
}

// specToModel populates the model from a MachineInstallDiskConfig resource read from Omni.
func (r *machineInstallDiskResource) specToModel(config *omni.MachineInstallDiskConfig, model *machineInstallDiskResourceModel) {
	model.MachineID = types.StringValue(config.Metadata().ID())

	spec := config.TypedSpec().Value

	// Omni reports the unused selection form as an empty string; map it back to null so the unset
	// attribute does not diff forever against the configuration.
	model.Disk = optionalString(spec.GetDisk())
	model.DiskSelector = optionalString(spec.GetDiskSelector())
}
