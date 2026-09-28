// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package connectapi

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/connect"
	"github.com/aws/aws-sdk-go-v2/service/connect/types"
)

// fakeModule is a module with its versions and aliases.
type fakeModule struct {
	module   types.ContactFlowModule
	versions map[int64]string // version -> content snapshot
	aliases  map[string]*types.ContactFlowModuleAliasInfo
	next     int64
}

func (f *Fake) mod(id string) (*fakeModule, error) {
	if f.modules == nil {
		f.modules = map[string]*fakeModule{}
	}
	m, ok := f.modules[id]
	if !ok {
		return nil, &types.ResourceNotFoundException{Message: aws.String(fmt.Sprintf("module %s not found", id))}
	}
	return m, nil
}

// Module returns a copy of a stored module, for assertions.
func (f *Fake) Module(id string) (types.ContactFlowModule, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.mod(id)
	if err != nil {
		return types.ContactFlowModule{}, false
	}
	return m.module, true
}

func (f *Fake) CreateContactFlowModule(_ context.Context, in *connect.CreateContactFlowModuleInput, _ ...func(*connect.Options)) (*connect.CreateContactFlowModuleOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("CreateContactFlowModule")
	if f.modules == nil {
		f.modules = map[string]*fakeModule{}
	}
	f.next++
	id := fmt.Sprintf("module-%d", f.next)
	arn := fmt.Sprintf("arn:aws:connect:us-east-1:111122223333:instance/%s/flow-module/%s", aws.ToString(in.InstanceId), id)
	tags := map[string]string{}
	for k, v := range in.Tags {
		tags[k] = v
	}
	f.modules[id] = &fakeModule{
		module: types.ContactFlowModule{
			Id: aws.String(id), Arn: aws.String(arn), Name: in.Name, Description: in.Description,
			Content: in.Content, State: types.ContactFlowModuleStateActive, Status: types.ContactFlowModuleStatusPublished,
			Tags: tags, ExternalInvocationConfiguration: in.ExternalInvocationConfiguration,
		},
		versions: map[int64]string{}, aliases: map[string]*types.ContactFlowModuleAliasInfo{},
	}
	return &connect.CreateContactFlowModuleOutput{Id: aws.String(id), Arn: aws.String(arn)}, nil
}

func (f *Fake) DescribeContactFlowModule(_ context.Context, in *connect.DescribeContactFlowModuleInput, _ ...func(*connect.Options)) (*connect.DescribeContactFlowModuleOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("DescribeContactFlowModule")
	m, err := f.mod(aws.ToString(in.ContactFlowModuleId))
	if err != nil {
		return nil, err
	}
	copied := m.module
	return &connect.DescribeContactFlowModuleOutput{ContactFlowModule: &copied}, nil
}

func (f *Fake) UpdateContactFlowModuleContent(_ context.Context, in *connect.UpdateContactFlowModuleContentInput, _ ...func(*connect.Options)) (*connect.UpdateContactFlowModuleContentOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("UpdateContactFlowModuleContent")
	m, err := f.mod(aws.ToString(in.ContactFlowModuleId))
	if err != nil {
		return nil, err
	}
	m.module.Content = in.Content
	return &connect.UpdateContactFlowModuleContentOutput{}, nil
}

func (f *Fake) UpdateContactFlowModuleMetadata(_ context.Context, in *connect.UpdateContactFlowModuleMetadataInput, _ ...func(*connect.Options)) (*connect.UpdateContactFlowModuleMetadataOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("UpdateContactFlowModuleMetadata")
	m, err := f.mod(aws.ToString(in.ContactFlowModuleId))
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		m.module.Name = in.Name
	}
	if in.Description != nil {
		m.module.Description = in.Description
	}
	if in.State != "" {
		m.module.State = in.State
	}
	return &connect.UpdateContactFlowModuleMetadataOutput{}, nil
}

func (f *Fake) DeleteContactFlowModule(_ context.Context, in *connect.DeleteContactFlowModuleInput, _ ...func(*connect.Options)) (*connect.DeleteContactFlowModuleOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("DeleteContactFlowModule")
	id := aws.ToString(in.ContactFlowModuleId)
	if _, err := f.mod(id); err != nil {
		return nil, err
	}
	delete(f.modules, id)
	return &connect.DeleteContactFlowModuleOutput{}, nil
}

func (f *Fake) CreateContactFlowModuleVersion(_ context.Context, in *connect.CreateContactFlowModuleVersionInput, _ ...func(*connect.Options)) (*connect.CreateContactFlowModuleVersionOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("CreateContactFlowModuleVersion")
	m, err := f.mod(aws.ToString(in.ContactFlowModuleId))
	if err != nil {
		return nil, err
	}
	m.next++
	m.versions[m.next] = aws.ToString(m.module.Content)
	return &connect.CreateContactFlowModuleVersionOutput{
		Version: aws.Int64(m.next), ContactFlowModuleArn: aws.String(fmt.Sprintf("%s:%d", aws.ToString(m.module.Arn), m.next)),
	}, nil
}

func (f *Fake) DeleteContactFlowModuleVersion(_ context.Context, in *connect.DeleteContactFlowModuleVersionInput, _ ...func(*connect.Options)) (*connect.DeleteContactFlowModuleVersionOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("DeleteContactFlowModuleVersion")
	m, err := f.mod(aws.ToString(in.ContactFlowModuleId))
	if err != nil {
		return nil, err
	}
	delete(m.versions, aws.ToInt64(in.ContactFlowModuleVersion))
	return &connect.DeleteContactFlowModuleVersionOutput{}, nil
}

func (f *Fake) CreateContactFlowModuleAlias(_ context.Context, in *connect.CreateContactFlowModuleAliasInput, _ ...func(*connect.Options)) (*connect.CreateContactFlowModuleAliasOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("CreateContactFlowModuleAlias")
	m, err := f.mod(aws.ToString(in.ContactFlowModuleId))
	if err != nil {
		return nil, err
	}
	id := fmt.Sprintf("alias-%s", aws.ToString(in.AliasName))
	arn := fmt.Sprintf("%s:%s", aws.ToString(m.module.Arn), aws.ToString(in.AliasName))
	m.aliases[id] = &types.ContactFlowModuleAliasInfo{
		AliasId: aws.String(id), ContactFlowModuleArn: aws.String(arn), ContactFlowModuleId: in.ContactFlowModuleId,
		Name: in.AliasName, Version: in.ContactFlowModuleVersion, Description: in.Description,
	}
	return &connect.CreateContactFlowModuleAliasOutput{Id: aws.String(id), ContactFlowModuleArn: aws.String(arn)}, nil
}

func (f *Fake) DescribeContactFlowModuleAlias(_ context.Context, in *connect.DescribeContactFlowModuleAliasInput, _ ...func(*connect.Options)) (*connect.DescribeContactFlowModuleAliasOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("DescribeContactFlowModuleAlias")
	m, err := f.mod(aws.ToString(in.ContactFlowModuleId))
	if err != nil {
		return nil, err
	}
	a, ok := m.aliases[aws.ToString(in.AliasId)]
	if !ok {
		return nil, &types.ResourceNotFoundException{Message: aws.String("alias not found")}
	}
	copied := *a
	return &connect.DescribeContactFlowModuleAliasOutput{ContactFlowModuleAlias: &copied}, nil
}

func (f *Fake) UpdateContactFlowModuleAlias(_ context.Context, in *connect.UpdateContactFlowModuleAliasInput, _ ...func(*connect.Options)) (*connect.UpdateContactFlowModuleAliasOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("UpdateContactFlowModuleAlias")
	m, err := f.mod(aws.ToString(in.ContactFlowModuleId))
	if err != nil {
		return nil, err
	}
	a, ok := m.aliases[aws.ToString(in.AliasId)]
	if !ok {
		return nil, &types.ResourceNotFoundException{Message: aws.String("alias not found")}
	}
	if in.ContactFlowModuleVersion != nil {
		a.Version = in.ContactFlowModuleVersion
	}
	if in.Description != nil {
		a.Description = in.Description
	}
	return &connect.UpdateContactFlowModuleAliasOutput{}, nil
}

func (f *Fake) DeleteContactFlowModuleAlias(_ context.Context, in *connect.DeleteContactFlowModuleAliasInput, _ ...func(*connect.Options)) (*connect.DeleteContactFlowModuleAliasOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("DeleteContactFlowModuleAlias")
	m, err := f.mod(aws.ToString(in.ContactFlowModuleId))
	if err != nil {
		return nil, err
	}
	delete(m.aliases, aws.ToString(in.AliasId))
	return &connect.DeleteContactFlowModuleAliasOutput{}, nil
}

// Views are what ListViews returns, set by a test.
func (f *Fake) SetViews(views []types.ViewSummary) {
	for _, v := range views {
		mustBePlaceholder(aws.ToString(v.Arn))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.views = views
}

func (f *Fake) ListViews(_ context.Context, in *connect.ListViewsInput, _ ...func(*connect.Options)) (*connect.ListViewsOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListViews")
	var all []types.ViewSummary
	for _, v := range f.views {
		if in.Type == "" || v.Type == in.Type {
			all = append(all, v)
		}
	}
	// MaxResults runs 1 to 100 here.
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_ListViews.html
	page, next, err := fakePage("ListViews", all, in.MaxResults, 100, in.NextToken)
	if err != nil {
		return nil, err
	}
	return &connect.ListViewsOutput{ViewsSummaryList: page, NextToken: next}, nil
}
