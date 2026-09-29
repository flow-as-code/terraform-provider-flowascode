---
page_title: "Promoting flows across environments"
subcategory: ""
description: |-
  One flow in a module, applied by each environment's root module, with a document hash that proves prod runs what dev ran.
---

# Promoting flows across environments

A flow is promoted when every environment runs the same document, each bound
to its own queues, hours of operation and functions. With this provider that
is one copy of the flow in a Terraform module, applied by each environment's
root module from the same commit.

In the module, the actions name what they use by key and `refs` binds each key
to a variable:

```terraform
resource "flowascode_contact_flow" "appointment_line" {
  instance_id = var.connect_instance_id
  name        = "appointment-line"
  type        = "CONTACT_FLOW"

  refs = {
    "queue:appointments" = var.appointments_queue_arn
  }

  # action blocks naming "queue:appointments"
}

output "appointment_line_document_sha256" {
  value = sha256(flowascode_contact_flow.appointment_line.flowdoc)
}
```

Each environment calls the module with its own addresses: a resource it
creates, a data source by name, or another team's remote state. The module
call is the only place environments differ.

`flowdoc` is the flow as a FlowDoc with its references still tokens, so its
hash is equal across environments exactly when the flows are, and it is known
at plan time. A pipeline can plan prod, read the hash from
`terraform show -json`, and refuse to apply unless it equals the hash dev
applied. `content_hash` would not do: it hashes the content Connect holds,
with each environment's ARNs filled in.

A shared module promotes the same way: flows bind `module:<name>@<alias>` to a
`flowascode_contact_flow_module_alias`, whose version is keyed to the module's
`content_hash` and needs `create_before_destroy`, because Connect refuses to
delete a version an alias points at.

The full walkthrough, with output from a live instance and a GitHub Actions
pipeline: [Promote a flow from dev to prod](https://flow-as-code.dev/docs/tutorial-promote/).
