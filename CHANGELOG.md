# Changelog

All notable changes to this provider. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/). Each release names the
flow-as-code `conformance/` commit it vendors and the FlowDoc format version
it reads.

## Unreleased

- Vendors flow-as-code `conformance/` at `650efbd` (provisional: that commit
  is on flow-as-code's unmerged `fix/compare-next-action` branch, so this is
  re-vendored from the merged `main` commit, with both TypeScript oracles
  re-recorded, before this entry is released). Amazon Connect refuses a
  `compare` action without a `next` ("Action is missing required property.
  Path: Actions[N].Transitions.NextAction", found on 2026-09-30), in every
  flow type probed, as the first action or later, and accepted every target
  tried; the console and flow-as-code's builder use the
  `NoMatchingCondition` branch's target. Plan-time lint gains the
  thirteenth rule, `next-action-required`: an error, not a hard rule, for a
  non-terminal action whose catalog `next` is `required` or `mirrors:*` and
  has none, shown as a plan warning. The refusal was checked on the types
  rule 38 lists as probed and is assumed for the fifteen that were not. For
  a mirrored type the message names the branch target to copy.
  `message_participant_iteratively` is not flagged, since the service
  accepts it either way. A `next` on a terminal action, which the service
  refuses, is not checked.
- Vendors flow-as-code `conformance/` at `9a2778b`, which adds
  `hcl/parse/null-attributes`: configuration from `-generate-config-out`, with
  `settings = null` on a flow and `next = null` on a terminal action. The
  provider already read these as unset; the case holds it to that.

## 0.1.1 (2026-09-29)

Vendors flow-as-code `conformance/` at
`12e484834f41b0c70b6b0499a8b542f0d4b401a6` and reads FlowDoc 0.2.

- Plan-time lint matches what Amazon Connect enforces when it creates a flow,
  found by creating every modeled action type in a sandbox instance on
  2026-09-29. Each case used to plan cleanly and fail at apply:
  `transfer_contact_to_queue` and `dequeue_contact_and_transfer_to_queue`
  need a `QueueAtCapacity` error block; `check_metric_data` needs
  `NoMatchingCondition` and at least one condition; `get_participant_input`
  needs `store_input`, and `NoMatchingCondition` whenever it is not "True";
  and `update_contact_recording_behavior` takes no error block at all (the
  service refuses `NoMatchingError`). An error block the action's type does
  not have is now reported too. The converse check found one rule too strict:
  `message_participant` no longer needs a `NoMatchingError` block, which the
  service does not require and Connect's own sample flows omit.
- The registry docs gain a guide, "Promoting flows across environments", and
  link the flow-as-code tutorials, cookbook and agent skills.
- An apply that Connect refuses shows its problem list. The service's
  `InvalidContactFlowException` often has an empty message; the list says
  which action is wrong and why ("Action is missing required error. Error:
  QueueAtCapacity, Path: Actions[1]").

## 0.1.0 (2026-09-29)

The first release. Vendors flow-as-code `conformance/` at
`c48da38c6aa3b718f524e22f087fd01eb4097b8d` and reads FlowDoc 0.2.

- Provider block on aws-sdk-go-base with hashicorp/aws's authentication
  vocabulary.
- `flowascode_contact_flow` and `flowascode_contact_flow_module`: flows
  authored as `action` blocks, one typed sub-block per modeled action type
  (built from the vendored catalog) or `generic {}` for the rest; references
  as keys bound through `refs`. The plan shows the canonical FlowDoc
  (`flowdoc`), the content Connect will hold (`content`) and its hash.
- `display_name` on both flow resources: the name Connect shows when it is
  not the slug in `name` (FlowDoc `displayName`). Import and `moved` keep a
  console name such as "Main Line" there and take its slug as `name`, so the
  first apply renames nothing.
- Lint at plan time: the twelve flow-as-code rules, hard rules as errors and
  the rest as warnings, with `lint { disable = [...] }` for the soft ones.
- Drift in Connect shows as changed action blocks; `terraform import` reads
  a live flow back as blocks, recovering `refs` bindings from the instance's
  inventory; `moved` from `aws_connect_contact_flow` and
  `aws_connect_contact_flow_module` (hashicorp/aws) without recreating.
- `flowascode_contact_flow_module_version`, keyed to the module's
  `content_hash` and refusing to snapshot a module whose live content
  differs, and `flowascode_contact_flow_module_alias`, repointed in place.
- `flowascode_view` data source.
- Compatibility: FlowDoc 0.2 (`flowdoc-0.2.schema.json`), Terraform 1.8 and
  later, OpenTofu 1.10 and later.
