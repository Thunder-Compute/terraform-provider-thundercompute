package resources

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// configTriggerAttributes are user-configurable attributes whose change may
// change computed instance fields either through update or replacement.
var configTriggerAttributes = []string{
	"cpu_cores", "disk_size_gb", "gpu_type", "mode", "num_gpus", "http_ports", "template", "public_key",
}

var instanceReplacementTriggerAttributes = []string{"template", "public_key"}

// --- String ---

type unknownStringOnConfigChange struct{}

func UnknownStringOnConfigChange() planmodifier.String {
	return unknownStringOnConfigChange{}
}

func (m unknownStringOnConfigChange) Description(_ context.Context) string {
	return "Marks value as unknown when configurable instance attributes change."
}

func (m unknownStringOnConfigChange) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m unknownStringOnConfigChange) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	if !anyConfigAttrChanged(ctx, req.Plan, req.State) {
		resp.PlanValue = req.StateValue
		return
	}
	resp.PlanValue = types.StringUnknown()
}

type unknownStringOnInstanceReplacement struct{}

func UnknownStringOnInstanceReplacement() planmodifier.String {
	return unknownStringOnInstanceReplacement{}
}

func (m unknownStringOnInstanceReplacement) Description(_ context.Context) string {
	return "Preserves the instance identity for in-place updates and marks it unknown when the instance is replaced."
}

func (m unknownStringOnInstanceReplacement) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m unknownStringOnInstanceReplacement) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	if anyAttributesChanged(ctx, req.Plan, req.State, instanceReplacementTriggerAttributes) {
		resp.PlanValue = types.StringUnknown()
		return
	}
	resp.PlanValue = req.StateValue
}

// --- Int64 ---

type unknownInt64OnConfigChange struct{}

func UnknownInt64OnConfigChange() planmodifier.Int64 {
	return unknownInt64OnConfigChange{}
}

func (m unknownInt64OnConfigChange) Description(_ context.Context) string {
	return "Marks value as unknown when configurable instance attributes change."
}

func (m unknownInt64OnConfigChange) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m unknownInt64OnConfigChange) PlanModifyInt64(ctx context.Context, req planmodifier.Int64Request, resp *planmodifier.Int64Response) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	if !anyConfigAttrChanged(ctx, req.Plan, req.State) {
		resp.PlanValue = req.StateValue
		return
	}
	resp.PlanValue = types.Int64Unknown()
}

// --- List ---

type unknownListOnConfigChange struct{}

func UnknownListOnConfigChange() planmodifier.List {
	return unknownListOnConfigChange{}
}

func (m unknownListOnConfigChange) Description(_ context.Context) string {
	return "Marks value as unknown when configurable instance attributes change."
}

func (m unknownListOnConfigChange) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m unknownListOnConfigChange) PlanModifyList(ctx context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	if !anyConfigAttrChanged(ctx, req.Plan, req.State) {
		resp.PlanValue = req.StateValue
		return
	}
	resp.PlanValue = types.ListUnknown(types.StringType)
}

// anyConfigAttrChanged compares trigger attributes between plan and state.
// GetAttribute diagnostics are intentionally not propagated: a missing attribute
// simply means "no change detected" for that field, which is the safe default.
// This is the expected behavior per the plugin framework -- plan modifiers should
// not emit diagnostics from attribute lookups that may not exist in all contexts.
func anyConfigAttrChanged(ctx context.Context, plan tfsdk.Plan, state tfsdk.State) bool {
	return anyAttributesChanged(ctx, plan, state, configTriggerAttributes)
}

func anyAttributesChanged(ctx context.Context, plan tfsdk.Plan, state tfsdk.State, attributes []string) bool {
	for _, attrName := range attributes {
		var planVal, stateVal attr.Value
		pDiags := plan.GetAttribute(ctx, path.Root(attrName), &planVal)
		sDiags := state.GetAttribute(ctx, path.Root(attrName), &stateVal)
		// Attribute not present in schema context -- skip safely
		if pDiags.HasError() || sDiags.HasError() {
			continue
		}
		if planVal == nil && stateVal == nil {
			continue
		}
		if planVal == nil || stateVal == nil {
			return true
		}
		if !planVal.Equal(stateVal) {
			return true
		}
	}
	return false
}
