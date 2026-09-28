---
page_title: "Migrating from hashicorp/aws"
subcategory: ""
description: |-
  Moving aws_connect_contact_flow and aws_connect_contact_flow_module resources to flowascode.
---

# Migrating from hashicorp/aws

A flow managed as `aws_connect_contact_flow` (or a module as
`aws_connect_contact_flow_module`) moves to this provider without being
recreated, in one of two ways.

## A `moved` block (Terraform 1.8, OpenTofu 1.10)

Write the flowascode resource, then tell Terraform the old one became it:

```hcl
moved {
  from = aws_connect_contact_flow.appointment_line
  to   = flowascode_contact_flow.appointment_line
}
```

The move copies the flow's identity and live attributes. The next plan writes
the actions from your configuration; when they describe the same flow, nothing
changes in Connect.

## Import with generated configuration

```hcl
import {
  to = flowascode_contact_flow.appointment_line
  id = "<instance_id>:<contact_flow_id>"
}
```

`terraform plan -generate-config-out=flows.tf` writes the flow as action
blocks. Its `refs` map binds each reference to the ARN the flow holds, found
in the instance's inventory: replace each with the resource address that
manages it (`aws_connect_queue.appointments.arn`). The flow-as-code studio
refuses a literal ARN in a flow, and so will review.

## From flow-as-code's `emit --target tf`

`flow-cli emit --target flowascode` writes the same flows as this provider's
resources. The emitter's `awscc_connect_contact_flow_module_version` and
`_alias` become `flowascode_contact_flow_module_version` and
`flowascode_contact_flow_module_alias`, which need no second provider.
