# terraform-provider-flowascode

The `flow-as-code/flowascode` provider for Terraform (1.8 and later) and
OpenTofu (1.10 and later): Amazon Connect contact flows authored natively in
HCL, one `action` block per action, created, updated and deleted through the
Amazon Connect API.

```hcl
resource "flowascode_contact_flow" "appointment_line" {
  instance_id = var.connect_instance_id
  name        = "appointment-line"
  type        = "CONTACT_FLOW"

  refs = {
    "queue:appointments" = aws_connect_queue.appointments.arn
  }

  action {
    id   = "welcome"
    next = "hang-up"
    message_participant {
      text = "Thanks for calling."
    }
    error {
      type = "NoMatchingError"
      next = "hang-up"
    }
  }

  action {
    id = "hang-up"
    disconnect_participant {}
  }
}
```

The resource shape is a contract shared with the TypeScript tooling in
[flow-as-code](https://github.com/flow-as-code/flow-as-code): its
`conformance/hcl/README.md` states every rule, and this repository vendors
that `conformance/` directory at a pinned commit and passes the same fixtures.
The flow-as-code studio reads and writes these resources as `.flow.tf`
companions.

Registry documentation is generated into `docs/` by tfplugindocs from the
schema, `templates/` and `examples/` (`go generate ./...`; CI fails when it
is stale), and every example is planned by the tests. Design decisions are in
`decisions/`.

Status: under construction. Nothing is published to a registry yet.

License: Apache-2.0.
