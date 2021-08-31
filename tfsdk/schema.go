package tfsdk

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

var (
	// ErrPathInsideAtomicAttribute is used with AttributeAtPath is called
	// on a path that doesn't have a schema associated with it, because
	// it's an element or attribute of a complex type, not a nested
	// attribute.
	ErrPathInsideAtomicAttribute = errors.New("path leads to element or attribute of a schema.Attribute that has no schema associated with it")
)

// Schema is used to define the shape of practitioner-provider information,
// like resources, data sources, and providers. Think of it as a type
// definition, but for Terraform.
type Schema struct {
	// Attributes are the fields inside the resource, provider, or data
	// source that the schema is defining. The map key should be the name
	// of the attribute, and the body defines how it behaves. Names must
	// only contain lowercase letters, numbers, and underscores.
	Attributes map[string]Attribute

	// Version indicates the current version of the schema. Schemas are
	// versioned to help with automatic upgrade process. This is not
	// typically required unless there is a change in the schema, such as
	// changing an attribute type, that needs manual upgrade handling.
	// Versions should only be incremented by one each release.
	Version int64

	DeprecationMessage  string
	Description         string
	MarkdownDescription string
}

// ApplyTerraform5AttributePathStep applies the given AttributePathStep to the
// schema.
func (s Schema) ApplyTerraform5AttributePathStep(step tftypes.AttributePathStep) (interface{}, error) {
	if v, ok := step.(tftypes.AttributeName); ok {
		if attr, ok := s.Attributes[string(v)]; ok {
			return attr, nil
		}
		return nil, fmt.Errorf("could not find attribute %q in schema", v)
	}
	return nil, fmt.Errorf("cannot apply AttributePathStep %T to schema", step)
}

// AttributeType returns a types.ObjectType composed from the schema types.
func (s Schema) AttributeType() attr.Type {
	attrTypes := map[string]attr.Type{}
	for name, attr := range s.Attributes {
		if attr.Type != nil {
			attrTypes[name] = attr.Type
		}
		if attr.Attributes != nil {
			attrTypes[name] = attr.Attributes.AttributeType()
		}
	}
	return types.ObjectType{AttrTypes: attrTypes}
}

// AttributeTypeAtPath returns the attr.Type of the attribute at the given path.
func (s Schema) AttributeTypeAtPath(path *tftypes.AttributePath) (attr.Type, error) {
	rawType, remaining, err := tftypes.WalkAttributePath(s, path)
	if err != nil {
		return nil, fmt.Errorf("%v still remains in the path: %w", remaining, err)
	}

	typ, ok := rawType.(attr.Type)
	if ok {
		return typ, nil
	}

	if n, ok := rawType.(nestedAttributes); ok {
		return n.AttributeType(), nil
	}

	a, ok := rawType.(Attribute)
	if !ok {
		return nil, fmt.Errorf("got unexpected type %T", rawType)
	}
	if a.Type != nil {
		return a.Type, nil
	}

	return a.Attributes.AttributeType(), nil
}

// TerraformType returns a tftypes.Type that can represent the schema.
func (s Schema) TerraformType(ctx context.Context) tftypes.Type {
	attrTypes := map[string]tftypes.Type{}
	for name, attr := range s.Attributes {
		if attr.Type != nil {
			attrTypes[name] = attr.Type.TerraformType(ctx)
		}
		if attr.Attributes != nil {
			attrTypes[name] = attr.Attributes.AttributeType().TerraformType(ctx)
		}
	}
	return tftypes.Object{AttributeTypes: attrTypes}
}

// AttributeAtPath returns the Attribute at the passed path. If the path points
// to an element or attribute of a complex type, rather than to an Attribute,
// it will return an ErrPathInsideAtomicAttribute error.
func (s Schema) AttributeAtPath(path *tftypes.AttributePath) (Attribute, error) {
	res, remaining, err := tftypes.WalkAttributePath(s, path)
	if err != nil {
		return Attribute{}, fmt.Errorf("%v still remains in the path: %w", remaining, err)
	}

	if _, ok := res.(attr.Type); ok {
		return Attribute{}, ErrPathInsideAtomicAttribute
	}

	a, ok := res.(Attribute)
	if !ok {
		return Attribute{}, fmt.Errorf("got unexpected type %T", res)
	}
	return a, nil
}

// tfprotov6Schema returns the *tfprotov6.Schema equivalent of a Schema. At least
// one attribute must be set in the schema, or an error will be returned.
func (s Schema) tfprotov6Schema(ctx context.Context) (*tfprotov6.Schema, error) {
	result := &tfprotov6.Schema{
		Version: s.Version,
	}

	var attrs []*tfprotov6.SchemaAttribute

	for name, attr := range s.Attributes {
		a, err := attr.tfprotov6SchemaAttribute(ctx, name, tftypes.NewAttributePath().WithAttributeName(name))

		if err != nil {
			return nil, err
		}

		attrs = append(attrs, a)
	}

	sort.Slice(attrs, func(i, j int) bool {
		if attrs[i] == nil {
			return true
		}

		if attrs[j] == nil {
			return false
		}

		return attrs[i].Name < attrs[j].Name
	})

	if len(attrs) < 1 {
		return nil, errors.New("must have at least one attribute in the schema")
	}

	result.Block = &tfprotov6.SchemaBlock{
		// core doesn't do anything with version, as far as I can tell,
		// so let's not set it.
		Attributes: attrs,
		Deprecated: s.DeprecationMessage != "",
	}

	if s.Description != "" {
		result.Block.Description = s.Description
		result.Block.DescriptionKind = tfprotov6.StringKindPlain
	}

	if s.MarkdownDescription != "" {
		result.Block.Description = s.MarkdownDescription
		result.Block.DescriptionKind = tfprotov6.StringKindMarkdown
	}

	return result, nil
}

// validate performs all Attribute validation.
func (s Schema) validate(ctx context.Context, req ValidateSchemaRequest, resp *ValidateSchemaResponse) {
	for name, attribute := range s.Attributes {
		attributeReq := ValidateAttributeRequest{
			AttributePath: tftypes.NewAttributePath().WithAttributeName(name),
			Config:        req.Config,
		}
		attributeResp := &ValidateAttributeResponse{
			Diagnostics: resp.Diagnostics,
		}

		attribute.validate(ctx, attributeReq, attributeResp)

		resp.Diagnostics = attributeResp.Diagnostics
	}

	if s.DeprecationMessage != "" {
		resp.Diagnostics = append(resp.Diagnostics, &tfprotov6.Diagnostic{
			Severity: tfprotov6.DiagnosticSeverityWarning,
			Summary:  "Deprecated",
			Detail:   s.DeprecationMessage,
		})
	}
}