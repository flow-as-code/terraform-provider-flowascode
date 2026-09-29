// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package connectapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/connect"
	"github.com/aws/aws-sdk-go-v2/service/connect/types"
)

// Fake is an in-memory Connect for tests: flows kept by id, with the
// behavior the resources rely on (NotFound for a missing flow, tags on the
// ARN). Account and instance ids are AWS's documentation placeholders.
type Fake struct {
	mu      sync.Mutex
	next    int
	flows   map[string]*types.ContactFlow
	modules map[string]*fakeModule
	views   []types.ViewSummary
	// Resources only the List* operations read, set by a test.
	queues  []types.QueueSummary
	hours   []types.HoursOfOperationSummary
	prompts []types.PromptSummary
	lambdas []string
	bots    []types.LexBotConfig
	// neverPublished holds the ids of flows that have only ever been saved.
	neverPublished map[string]bool
	// Calls records every operation, in order, for assertions.
	Calls []string
}

// NewFake returns an empty fake.
func NewFake() *Fake { return &Fake{flows: map[string]*types.ContactFlow{}} }

var _ API = (*Fake)(nil)

func notFound(id string) error {
	return &types.ResourceNotFoundException{Message: aws.String(fmt.Sprintf("flow %s not found", id))}
}

// Flow returns a copy of a stored flow, for assertions and for simulating an
// out-of-band edit through SetContent.
func (f *Fake) Flow(id string) (types.ContactFlow, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.flows[id]
	if !ok {
		return types.ContactFlow{}, false
	}
	return *c, true
}

// SetContent replaces a flow's content, as a console edit would.
func (f *Fake) SetContent(id, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flows[id].Content = aws.String(content)
}

func (f *Fake) record(op string) { f.Calls = append(f.Calls, op) }

func (f *Fake) DescribeInstance(context.Context, *connect.DescribeInstanceInput, ...func(*connect.Options)) (*connect.DescribeInstanceOutput, error) {
	return &connect.DescribeInstanceOutput{}, nil
}

func (f *Fake) CreateContactFlow(_ context.Context, in *connect.CreateContactFlowInput, _ ...func(*connect.Options)) (*connect.CreateContactFlowOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("CreateContactFlow")
	if problems := refusedContent(aws.ToString(in.Content)); len(problems) > 0 {
		return nil, &types.InvalidContactFlowException{Problems: problems}
	}
	f.next++
	id := fmt.Sprintf("flow-%d", f.next)
	arn := fmt.Sprintf("arn:aws:connect:us-east-1:111122223333:instance/%s/contact-flow/%s", aws.ToString(in.InstanceId), id)
	tags := map[string]string{}
	for k, v := range in.Tags {
		tags[k] = v
	}
	f.flows[id] = &types.ContactFlow{
		Id: aws.String(id), Arn: aws.String(arn), Name: in.Name, Description: in.Description,
		Type: in.Type, Content: in.Content, State: types.ContactFlowStateActive, Status: in.Status, Tags: tags,
	}
	return &connect.CreateContactFlowOutput{ContactFlowId: aws.String(id), ContactFlowArn: aws.String(arn)}, nil
}

func (f *Fake) DescribeContactFlow(_ context.Context, in *connect.DescribeContactFlowInput, _ ...func(*connect.Options)) (*connect.DescribeContactFlowOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("DescribeContactFlow")
	// "Use the $SAVED alias in the request to describe the SAVED content of a
	// Flow." A never-published flow is readable only through it.
	// https://docs.aws.amazon.com/connect/latest/APIReference/API_DescribeContactFlow.html
	id, saved := strings.CutSuffix(aws.ToString(in.ContactFlowId), ":$SAVED")
	c, ok := f.flows[id]
	if !ok {
		return nil, notFound(aws.ToString(in.ContactFlowId))
	}
	if f.neverPublished[id] && !saved {
		return nil, &types.ContactFlowNotPublishedException{Message: aws.String(fmt.Sprintf("flow %s has not been published", id))}
	}
	copied := *c
	return &connect.DescribeContactFlowOutput{ContactFlow: &copied}, nil
}

func (f *Fake) UpdateContactFlowContent(_ context.Context, in *connect.UpdateContactFlowContentInput, _ ...func(*connect.Options)) (*connect.UpdateContactFlowContentOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("UpdateContactFlowContent")
	if problems := refusedContent(aws.ToString(in.Content)); len(problems) > 0 {
		return nil, &types.InvalidContactFlowException{Problems: problems}
	}
	c, ok := f.flows[aws.ToString(in.ContactFlowId)]
	if !ok {
		return nil, notFound(aws.ToString(in.ContactFlowId))
	}
	c.Content = in.Content
	return &connect.UpdateContactFlowContentOutput{}, nil
}

func (f *Fake) UpdateContactFlowMetadata(_ context.Context, in *connect.UpdateContactFlowMetadataInput, _ ...func(*connect.Options)) (*connect.UpdateContactFlowMetadataOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("UpdateContactFlowMetadata")
	c, ok := f.flows[aws.ToString(in.ContactFlowId)]
	if !ok {
		return nil, notFound(aws.ToString(in.ContactFlowId))
	}
	if in.Name != nil {
		c.Name = in.Name
	}
	if in.Description != nil {
		c.Description = in.Description
	}
	if in.ContactFlowState != "" {
		c.State = in.ContactFlowState
	}
	return &connect.UpdateContactFlowMetadataOutput{}, nil
}

func (f *Fake) DeleteContactFlow(_ context.Context, in *connect.DeleteContactFlowInput, _ ...func(*connect.Options)) (*connect.DeleteContactFlowOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("DeleteContactFlow")
	id := aws.ToString(in.ContactFlowId)
	if _, ok := f.flows[id]; !ok {
		return nil, notFound(id)
	}
	delete(f.flows, id)
	return &connect.DeleteContactFlowOutput{}, nil
}

func (f *Fake) byArn(arn string) *types.ContactFlow {
	for _, c := range f.flows {
		if aws.ToString(c.Arn) == arn {
			return c
		}
	}
	return nil
}

func (f *Fake) TagResource(_ context.Context, in *connect.TagResourceInput, _ ...func(*connect.Options)) (*connect.TagResourceOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("TagResource")
	c := f.byArn(aws.ToString(in.ResourceArn))
	if c == nil {
		return nil, notFound(aws.ToString(in.ResourceArn))
	}
	for k, v := range in.Tags {
		c.Tags[k] = v
	}
	return &connect.TagResourceOutput{}, nil
}

func (f *Fake) UntagResource(_ context.Context, in *connect.UntagResourceInput, _ ...func(*connect.Options)) (*connect.UntagResourceOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("UntagResource")
	c := f.byArn(aws.ToString(in.ResourceArn))
	if c == nil {
		return nil, notFound(aws.ToString(in.ResourceArn))
	}
	for _, k := range in.TagKeys {
		delete(c.Tags, k)
	}
	return &connect.UntagResourceOutput{}, nil
}

// refusedContent is the part of Connect's content validation the tests
// exercise: a queue transfer without its QueueAtCapacity branch, which the
// service refuses with an empty message and one problem per action
// ("Action is missing required error. Error: QueueAtCapacity, Path:
// Actions[1]", sandbox, 2026-09-29). Anything else is accepted, as before.
func refusedContent(content string) []types.ProblemDetail {
	var doc struct {
		Actions []struct {
			Type        string
			Transitions struct {
				Errors []struct{ ErrorType string }
			}
		}
	}
	if json.Unmarshal([]byte(content), &doc) != nil {
		return nil
	}
	var out []types.ProblemDetail
	for i, a := range doc.Actions {
		if a.Type != "TransferContactToQueue" && a.Type != "DequeueContactAndTransferToQueue" {
			continue
		}
		wired := false
		for _, e := range a.Transitions.Errors {
			wired = wired || e.ErrorType == "QueueAtCapacity"
		}
		if !wired {
			out = append(out, types.ProblemDetail{Message: aws.String(fmt.Sprintf("Action is missing required error. Error: QueueAtCapacity, Path: Actions[%d]", i))})
		}
	}
	return out
}
