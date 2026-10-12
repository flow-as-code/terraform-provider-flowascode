// Copyright 2026 The flow-as-code Authors
// SPDX-License-Identifier: Apache-2.0

package connectapi

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/connect"
	"github.com/aws/aws-sdk-go-v2/service/connect/types"
	casestypes "github.com/aws/aws-sdk-go-v2/service/connectcases/types"

	"github.com/flow-as-code/terraform-provider-flowascode/internal/export"
)

// phoneNumberSpy records ListPhoneNumbersV2's inputs.
type phoneNumberSpy struct {
	*Fake
	inputs []*connect.ListPhoneNumbersV2Input
}

func (s *phoneNumberSpy) ListPhoneNumbersV2(ctx context.Context, in *connect.ListPhoneNumbersV2Input, opts ...func(*connect.Options)) (*connect.ListPhoneNumbersV2Output, error) {
	s.inputs = append(s.inputs, in)
	return s.Fake.ListPhoneNumbersV2(ctx, in, opts...)
}

// ListPhoneNumbersV2 takes the instance as InstanceId for an id and as
// TargetArn for an instance ARN, never both, as export.ts sends it.
func TestListPhoneNumbersNamesTheInstanceOneWay(t *testing.T) {
	for _, instance := range []string{testInstanceID, testInstance} {
		s := &phoneNumberSpy{Fake: NewFake()}
		s.SetPhoneNumbers([]types.ListPhoneNumbersSummary{{
			PhoneNumberArn: aws.String("arn:aws:connect:us-east-1:111122223333:phone-number/p1"),
			PhoneNumberId:  aws.String("p1"), PhoneNumber: aws.String("+15555550100"), PhoneNumberType: types.PhoneNumberTypeDid,
		}})
		got, err := NewInventoryWithOptions(s, instance, InventoryOptions{RequestsPerSecond: noLimit()}).(export.PhoneNumberLister).ListPhoneNumbers(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		want := []export.PhoneNumberSummary{{Arn: "arn:aws:connect:us-east-1:111122223333:phone-number/p1", ID: "p1", Number: "+15555550100", Type: aws.String("DID")}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %+v", instance, got)
		}
		in := s.inputs[0]
		if instance == testInstance && (in.TargetArn == nil || in.InstanceId != nil) {
			t.Errorf("an instance ARN went as InstanceId %v, TargetArn %v", in.InstanceId, in.TargetArn)
		}
		if instance == testInstanceID && (in.InstanceId == nil || in.TargetArn != nil) {
			t.Errorf("an instance id went as InstanceId %v, TargetArn %v", in.InstanceId, in.TargetArn)
		}
	}
}

// Without a Cases or Q in Connect client the adapter reads neither: no
// integration association is listed, and the inventory has no domain and no
// assistants (absent, not empty), as createConnectInventoryClient offers no
// describeCasesDomain or listAssistants without `cases` or `qconnect`. The
// two Connect lists are read either way.
func TestCasesAndAssistantsNeedTheirClients(t *testing.T) {
	f := NewFake()
	f.SetIntegrationAssociations([]types.IntegrationAssociationSummary{{
		IntegrationArn: aws.String("arn:aws:cases:us-east-1:111122223333:domain/d1"), IntegrationType: types.IntegrationTypeCasesDomain,
	}})
	inv, err := export.CollectInventory(context.Background(), NewInventoryWithOptions(f, testInstanceID, InventoryOptions{RequestsPerSecond: noLimit()}), export.CollectInventoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if inv.CasesDomain != nil || inv.Assistants != nil {
		t.Errorf("domain %+v, assistants %+v", inv.CasesDomain, inv.Assistants)
	}
	if inv.TaskTemplates == nil || inv.PhoneNumbers == nil {
		t.Errorf("the Connect lists were not read: %v %v", inv.TaskTemplates, inv.PhoneNumbers)
	}
	if slices.Contains(f.Calls, "ListIntegrationAssociations") {
		t.Errorf("listed integration associations without a Cases client: %v", f.Calls)
	}
}

// With a Cases client, the domain is the CASES_DOMAIN association's
// IntegrationArn; an association of another type, or none, is no domain,
// and the Cases service is then not called.
func TestCasesDomainComesFromItsAssociation(t *testing.T) {
	domain := "arn:aws:cases:us-east-1:111122223333:domain/d1"
	for _, c := range []struct {
		name         string
		associations []types.IntegrationAssociationSummary
		want         bool
	}{
		{"none", nil, false},
		{"another type", []types.IntegrationAssociationSummary{{IntegrationArn: aws.String(domain), IntegrationType: types.IntegrationTypeWisdomAssistant}}, false},
		{"a non-Cases ARN first", []types.IntegrationAssociationSummary{
			{IntegrationArn: aws.String("arn:aws:connect:us-east-1:111122223333:instance/x"), IntegrationType: types.IntegrationTypeCasesDomain},
			{IntegrationArn: aws.String(domain), IntegrationType: types.IntegrationTypeCasesDomain},
		}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := NewFake()
			f.SetIntegrationAssociations(c.associations)
			cases := &FakeCases{DomainID: "d1", Fields: []casestypes.FieldSummary{{FieldArn: aws.String(domain + "/field/f1"), FieldId: aws.String("f1"), Name: aws.String("Priority")}}}
			got, err := NewInventoryWithOptions(f, testInstanceID, InventoryOptions{RequestsPerSecond: noLimit(), Cases: cases}).(export.CasesDomainDescriber).DescribeCasesDomain(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !c.want {
				if got != nil || len(cases.Calls) != 0 {
					t.Errorf("domain %+v, Cases calls %v", got, cases.Calls)
				}
				return
			}
			want := &export.CasesDomainInventory{DomainArn: domain, DomainID: "d1", Templates: []export.ResourceSummary{},
				Fields: []export.ResourceSummary{{Arn: domain + "/field/f1", ID: aws.String("f1"), Name: "Priority"}}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got %+v", got)
			}
		})
	}
}
