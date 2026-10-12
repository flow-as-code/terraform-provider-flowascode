---
page_title: "Permissions"
subcategory: ""
description: |-
  The Amazon Connect operations the provider calls, and the IAM actions they need.
---

# Permissions

Each Amazon Connect operation the provider calls needs the IAM action of the
same name in the `connect` service, `connect:<Operation>`, allowed on the
instance's resources. The actions, their resource types and their condition
keys are listed in the
[Service Authorization Reference for Amazon Connect](https://docs.aws.amazon.com/service-authorization/latest/reference/list_amazonconnect.html).

## Managing flows and modules

- `flowascode_contact_flow`: `CreateContactFlow`, `DescribeContactFlow`,
  `UpdateContactFlowContent`, `UpdateContactFlowMetadata` and
  `DeleteContactFlow`.
- `flowascode_contact_flow_module`: `CreateContactFlowModule`,
  `DescribeContactFlowModule`, `UpdateContactFlowModuleContent`,
  `UpdateContactFlowModuleMetadata` and `DeleteContactFlowModule`.
- Module versions and aliases: `CreateContactFlowModuleVersion`,
  `DeleteContactFlowModuleVersion`, `CreateContactFlowModuleAlias`,
  `DescribeContactFlowModuleAlias`, `UpdateContactFlowModuleAlias`,
  `DeleteContactFlowModuleAlias` and `ListContactFlowModuleAliases`.
- Tags: `TagResource` and `UntagResource`.
- The `flowascode_view` data source: `ListViews`.

## Reading a live flow back

An import, or a refresh after the flow changed outside Terraform, shows the
live flow as action blocks. Every ARN in it is named from the `refs` already
in state; when one is not, the provider lists the instance once and names it
from what it finds. That listing calls:

| Operation | Names |
| --- | --- |
| `ListContactFlows` | flows |
| `ListContactFlowModules`, `ListContactFlowModuleAliases` | modules and their aliases |
| `ListQueues` | queues |
| `ListHoursOfOperations` | hours of operation |
| `ListPrompts` | prompts |
| `ListLambdaFunctions` | Lambda functions |
| `ListBots` | Amazon Lex bots |
| `ListViews` | views |
| `ListTaskTemplates` | task templates (FlowDoc 0.3) |
| `ListPhoneNumbersV2` | claimed phone numbers (FlowDoc 0.3) |

When the listing is refused, the plan says so in a warning and leaves the
action blocks as they were (none, after an import), while `content` still
shows the change; bind each ARN in `refs` to see the live blocks.

This provider does not list Cases templates and fields or Amazon Q in
Connect assistants, so it needs no `cases:` or `wisdom:` permission. A
Cases template or field ARN, or an assistant ARN, in a live flow is named
only through a binding in `refs`.
