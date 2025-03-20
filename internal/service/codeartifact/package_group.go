// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package codeartifact

import (
	"context"
	"errors"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	fwtypes "github.com/hashicorp/terraform-provider-aws/internal/framework/types"
	tftags "github.com/hashicorp/terraform-provider-aws/internal/tags"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/codeartifact"
	awstypes "github.com/aws/aws-sdk-go-v2/service/codeartifact/types"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
	"github.com/hashicorp/terraform-provider-aws/internal/create"
	"github.com/hashicorp/terraform-provider-aws/internal/errs"
	"github.com/hashicorp/terraform-provider-aws/internal/errs/fwdiag"
	"github.com/hashicorp/terraform-provider-aws/internal/framework"
	"github.com/hashicorp/terraform-provider-aws/internal/framework/flex"
	"github.com/hashicorp/terraform-provider-aws/internal/tfresource"
	"github.com/hashicorp/terraform-provider-aws/names"
)

// Function annotations are used for resource registration to the Provider. DO NOT EDIT.
// @FrameworkResource("aws_codeartifact_package_group", name="Package Group")
// @Tags(identifierAttribute="arn")
func newResourcePackageGroup(_ context.Context) (resource.ResourceWithConfigure, error) {
	r := &resourcePackageGroup{}

	r.SetDefaultCreateTimeout(30 * time.Minute)
	r.SetDefaultUpdateTimeout(30 * time.Minute)
	r.SetDefaultDeleteTimeout(30 * time.Minute)

	return r, nil
}

const (
	ResNamePackageGroup = "Package Group"
)

type resourcePackageGroup struct {
	framework.ResourceWithConfigure
	framework.WithTimeouts
}

func (r *resourcePackageGroup) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			names.AttrARN: framework.ARNAttributeComputedOnly(),
			names.AttrDescription: schema.StringAttribute{
				Optional: true,
			},
			names.AttrDomain: schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthBetween(2, 50),
				},
			},
			"domain_owner": schema.StringAttribute{
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						regexp.MustCompile(`^\d{12}$`),
						"must be a valid AWS account ID (12-digit numeric string)"),
				},
			},
			"contact_info": schema.StringAttribute{
				Optional: true,
				Validators: []validator.String{
					stringvalidator.LengthAtMost(1000),
				},
			},
			"package_group": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthBetween(2, 1000),
				},
			},
			names.AttrTags:    tftags.TagsAttribute(),
			names.AttrTagsAll: tftags.TagsAttributeComputedOnly(),
		},
		Blocks: map[string]schema.Block{
			"origin_configuration": schema.ListNestedBlock{
				CustomType: fwtypes.NewListNestedObjectTypeOf[originConfigurationModel](ctx),
				Validators: []validator.List{
					listvalidator.SizeAtMost(1),
				},
				NestedObject: schema.NestedBlockObject{
					Blocks: map[string]schema.Block{
						"restriction": schema.SetNestedBlock{
							CustomType: fwtypes.NewSetNestedObjectTypeOf[restrictionModel](ctx),
							Validators: []validator.Set{
								setvalidator.SizeAtMost(3),
							},
							NestedObject: schema.NestedBlockObject{
								Attributes: map[string]schema.Attribute{
									"type": schema.StringAttribute{
										Required:    true,
										Description: "Type of restriction (EXTERNAL_UPSTREAM, INTERNAL_UPSTREAM, PUBLISH)",
									},
									"mode": schema.StringAttribute{
										Optional: true,
										Computed: true,
									},
									"repositories": schema.SetAttribute{
										ElementType: types.StringType,
										Optional:    true,
									},
								},
							},
						},
					},
				},
			},
			names.AttrTimeouts: timeouts.Block(ctx, timeouts.Opts{
				Create: true,
				Update: true,
				Delete: true,
			}),
		},
	}
}

func (r *resourcePackageGroup) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	conn := r.Meta().CodeArtifactClient(ctx)

	var plan resourcePackageGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var input codeartifact.CreatePackageGroupInput
	resp.Diagnostics.Append(flex.Expand(ctx, plan, &input)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := conn.CreatePackageGroup(ctx, &input)
	if err != nil {
		resp.Diagnostics.AddError(
			create.ProblemStandardMessage(names.CodeArtifact, create.ErrActionCreating, ResNamePackageGroup, plan.PackageGroup.String(), err),
			err.Error(),
		)
		return
	}
	if out == nil || out.PackageGroup == nil {
		resp.Diagnostics.AddError(
			create.ProblemStandardMessage(names.CodeArtifact, create.ErrActionCreating, ResNamePackageGroup, plan.PackageGroup.String(), nil),
			errors.New("empty output").Error(),
		)
		return
	}

	//resp.Diagnostics.Append(flex.Flatten(ctx, out.PackageGroup, &plan)...)
	//if resp.Diagnostics.HasError() {
	//	return
	//}

	// Add the origin controls
	var originConfig []originConfigurationModel
	resp.Diagnostics.Append(plan.OriginConfiguration.ElementsAs(ctx, &originConfig, false)...)

	var restrictions []restrictionModel
	resp.Diagnostics.Append(originConfig[0].Restrictions.ElementsAs(ctx, &restrictions, false)...)

	restrictionsMap := make(map[string]awstypes.PackageGroupOriginRestrictionMode)
	for _, restriction := range restrictions {
		restrictionsMap[restriction.MapBlockKey.ValueString()] = awstypes.PackageGroupOriginRestrictionMode(restriction.Mode.ValueString()) // ✅ Use Type as the key
	}

	var allowedRepositories []awstypes.PackageGroupAllowedRepository
	var repositories []types.String

	for _, restriction := range restrictions {
		restrictionsMap[restriction.MapBlockKey.ValueString()] = awstypes.PackageGroupOriginRestrictionMode(restriction.Mode.ValueString())
		//ToDO: Check for empty/null
		restriction.Repositories.ElementsAs(ctx, &repositories, false)
		for _, repo := range repositories {
			allowedRepositories = append(allowedRepositories, awstypes.PackageGroupAllowedRepository{
				OriginRestrictionType: awstypes.PackageGroupOriginRestrictionType(restriction.MapBlockKey.ValueString()), // ✅ Use restriction type
				RepositoryName:        aws.String(repo.ValueString()),                                                    // ✅ Use repository name
			})
		}
	}

	var packageInput = codeartifact.UpdatePackageGroupOriginConfigurationInput{
		Domain:                 out.PackageGroup.DomainName,
		DomainOwner:            out.PackageGroup.DomainOwner,
		PackageGroup:           out.PackageGroup.Pattern,
		Restrictions:           restrictionsMap,
		AddAllowedRepositories: allowedRepositories,
	}
	packageOut, err := conn.UpdatePackageGroupOriginConfiguration(ctx, &packageInput)

	if err != nil {
		resp.Diagnostics.AddError(
			create.ProblemStandardMessage(names.CodeArtifact, create.ErrActionCreating, ResNamePackageGroup, plan.PackageGroup.String(), err),
			err.Error(),
		)
		return
	}
	if packageOut == nil || packageOut.PackageGroup == nil {
		resp.Diagnostics.AddError(
			create.ProblemStandardMessage(names.CodeArtifact, create.ErrActionCreating, ResNamePackageGroup, plan.PackageGroup.String(), nil),
			errors.New("empty output").Error(),
		)
		return
	}

	//resp.Diagnostics.Append(flex.Flatten(ctx, out.PackageGroup, &plan)...)
	//if resp.Diagnostics.HasError() {
	//	return
	//}

	// to here

	// refresh
	updatedOut, err := findPackageGroupByThreePartKey(ctx, conn, plan.Domain.ValueString(), plan.DomainOwner.ValueString(), plan.PackageGroup.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			create.ProblemStandardMessage(names.CodeArtifact, create.ErrActionReading, ResNamePackageGroup, plan.PackageGroup.String(), err),
			err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(flex.Flatten(ctx, updatedOut.PackageGroup, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// end additional calls

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *resourcePackageGroup) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	conn := r.Meta().CodeArtifactClient(ctx)

	var state resourcePackageGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := findPackageGroupByThreePartKey(ctx, conn, state.Domain.ValueString(), state.DomainOwner.ValueString(), state.PackageGroup.ValueString())
	if tfresource.NotFound(err) {
		resp.Diagnostics.Append(fwdiag.NewResourceNotFoundWarningDiagnostic(err))
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(
			create.ProblemStandardMessage(names.CodeArtifact, create.ErrActionReading, ResNamePackageGroup, state.ARN.String(), err),
			err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(flex.Flatten(ctx, out, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *resourcePackageGroup) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	conn := r.Meta().CodeArtifactClient(ctx)

	var plan, state resourcePackageGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	diff, d := flex.Diff(ctx, plan, state)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	if diff.HasChanges() {
		var input codeartifact.UpdatePackageGroupInput
		resp.Diagnostics.Append(flex.Expand(ctx, plan, &input)...)
		if resp.Diagnostics.HasError() {
			return
		}

		out, err := conn.UpdatePackageGroup(ctx, &input)
		if err != nil {
			resp.Diagnostics.AddError(
				create.ProblemStandardMessage(names.CodeArtifact, create.ErrActionUpdating, ResNamePackageGroup, plan.ARN.String(), err),
				err.Error(),
			)
			return
		}
		if out == nil || out.PackageGroup == nil {
			resp.Diagnostics.AddError(
				create.ProblemStandardMessage(names.CodeArtifact, create.ErrActionUpdating, ResNamePackageGroup, plan.ARN.String(), nil),
				errors.New("empty output").Error(),
			)
			return
		}

		resp.Diagnostics.Append(flex.Flatten(ctx, out, &plan)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *resourcePackageGroup) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	conn := r.Meta().CodeArtifactClient(ctx)

	var state resourcePackageGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := codeartifact.DeletePackageGroupInput{
		Domain:       state.Domain.ValueStringPointer(),
		PackageGroup: state.PackageGroup.ValueStringPointer(),
	}

	if state.DomainOwner.ValueString() != "" {
		input.DomainOwner = state.DomainOwner.ValueStringPointer()
	}

	_, err := conn.DeletePackageGroup(ctx, &input)
	if err != nil {
		if errs.IsA[*awstypes.ResourceNotFoundException](err) {
			return
		}

		resp.Diagnostics.AddError(
			create.ProblemStandardMessage(names.CodeArtifact, create.ErrActionDeleting, ResNamePackageGroup, state.ARN.String(), err),
			err.Error(),
		)
		return
	}
}

func (r *resourcePackageGroup) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root(names.AttrID), req, resp)
}

func findPackageGroupByThreePartKey(ctx context.Context, conn *codeartifact.Client, domain string, domainOwner string, packageGroup string) (*codeartifact.DescribePackageGroupOutput, error) {
	input := codeartifact.DescribePackageGroupInput{
		Domain:       aws.String(domain),
		PackageGroup: aws.String(packageGroup),
	}

	if domainOwner != "" {
		input.DomainOwner = aws.String(domainOwner)
	}

	out, err := conn.DescribePackageGroup(ctx, &input)
	if err != nil {
		if errs.IsA[*awstypes.ResourceNotFoundException](err) {
			return nil, &retry.NotFoundError{
				LastError:   err,
				LastRequest: &input,
			}
		}

		return nil, err
	}

	if out == nil || out.PackageGroup == nil {
		return nil, tfresource.NewEmptyResultError(&input)
	}

	return out, nil
}

type resourcePackageGroupModel struct {
	ARN                 types.String                                              `tfsdk:"arn"`
	ContactInfo         types.String                                              `tfsdk:"contact_info"`
	Description         types.String                                              `tfsdk:"description"`
	Domain              types.String                                              `tfsdk:"domain"`
	DomainOwner         types.String                                              `tfsdk:"domain_owner"`
	PackageGroup        types.String                                              `tfsdk:"package_group"`
	Tags                tftags.Map                                                `tfsdk:"tags"`
	TagsAll             tftags.Map                                                `tfsdk:"tags_all"`
	Timeouts            timeouts.Value                                            `tfsdk:"timeouts"`
	OriginConfiguration fwtypes.ListNestedObjectValueOf[originConfigurationModel] `tfsdk:"origin_configuration"`
}

type originConfigurationModel struct {
	Restrictions fwtypes.SetNestedObjectValueOf[restrictionModel] `tfsdk:"restriction"`
}

// New struct for restrictions
type restrictionModel struct {
	MapBlockKey  types.String                     `tfsdk:"type"`
	Mode         types.String                     `tfsdk:"mode"`
	Repositories fwtypes.SetValueOf[types.String] `tfsdk:"repositories"`
}

type packageOriginConfigurationModel struct {
	Domain       types.String            `tfsdk:"domain"`
	DomainOwner  types.String            `tfsdk:"domain_owner"`
	PackageGroup types.String            `tfsdk:"package_group"`
	Restrictions map[string]types.String `tfsdk:"restrictions"` // ✅ Ensure it matches AWS format
}
