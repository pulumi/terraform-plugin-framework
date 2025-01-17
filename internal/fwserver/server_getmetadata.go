// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package fwserver

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/internal/logging"
	"github.com/hashicorp/terraform-plugin-framework/provider"
)

// GetMetadataRequest is the framework server request for the
// GetMetadata RPC.
type GetMetadataRequest struct{}

// GetMetadataResponse is the framework server response for the
// GetMetadata RPC.
type GetMetadataResponse struct {
	DataSources        []DataSourceMetadata
	Diagnostics        diag.Diagnostics
	Resources          []ResourceMetadata
	ServerCapabilities *ServerCapabilities
}

// DataSourceMetadata is the framework server equivalent of the
// tfprotov5.DataSourceMetadata and tfprotov6.DataSourceMetadata types.
type DataSourceMetadata struct {
	// TypeName is the name of the data resource.
	TypeName string
}

// ResourceMetadata is the framework server equivalent of the
// tfprotov5.ResourceMetadata and tfprotov6.ResourceMetadata types.
type ResourceMetadata struct {
	// TypeName is the name of the managed resource.
	TypeName string
}

// GetMetadata implements the framework server GetMetadata RPC.
func (s *Server) GetMetadata(ctx context.Context, req *GetMetadataRequest, resp *GetMetadataResponse) {
	resp.DataSources = []DataSourceMetadata{}
	resp.Resources = []ResourceMetadata{}
	resp.ServerCapabilities = s.ServerCapabilities()

	metadataReq := provider.MetadataRequest{}
	metadataResp := provider.MetadataResponse{}

	logging.FrameworkTrace(ctx, "Calling provider defined Provider Metadata")
	s.Provider.Metadata(ctx, metadataReq, &metadataResp)
	logging.FrameworkTrace(ctx, "Called provider defined Provider Metadata")

	s.providerTypeName = metadataResp.TypeName

	datasourceMetadatas, diags := s.DataSourceMetadatas(ctx)

	resp.Diagnostics.Append(diags...)

	resourceMetadatas, diags := s.ResourceMetadatas(ctx)

	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	resp.DataSources = datasourceMetadatas
	resp.Resources = resourceMetadatas
}
