# Changelog

All notable changes to this provider. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/). Each release names the
flow-as-code `conformance/` commit it vendors and the FlowDoc format version
it reads.

## Unreleased

Vendors flow-as-code `conformance/` at
`78b0a87d01992d532624d40a89686400c19b512c` (the merge of flow-as-code #35,
the last of its Phase C batch) and reads FlowDoc 0.2.

- Two lint rules, fifteen in all. `channel-restricted-action` is a warning
  on `wait` and `show_view`, whose action pages restrict them to the chat
  channel; a document does not record which channels its flow serves, so
  the message names the channels and the page, and a chat-only flow
  disables the rule in its `lint { disable }` block.
  `attribute-set-before-read` is a warning on a `$.Attributes.<name>` read
  in message text when no document in the planned set writes `<name>`
  through `update_contact_attributes` on the current contact (a
  `TargetContact` of `Related` does not count); it reads across the set
  and says nothing for a resource planned alone, as `module-depth-5` does.
- `maximum_length` in `get_participant_input`'s `input_validation.
  custom_validation` is a String attribute; it was a Number. The catalog
  now records the kind the console writes (`integerString`), so the
  deployed content carries `MaximumLength` as a string. A configuration
  that writes `maximum_length = 5` keeps working, since Terraform converts
  the number. Upgrade note: a state written by 0.1.x reads without a state
  upgrader (the number becomes its digits), and the first plan after
  upgrading shows one in-place update of `content`, `content_hash` and
  `flowdoc` for each flow or module with a stored-input block, the string
  form being sent on apply; it is not a replacement. A flow whose
  `store_input` is "False" is not affected. `InvalidPhoneNumber` is now a
  builder error branch of `get_participant_input`, before
  `NoMatchingError`.
- The catalog gains `channels` on a modeled entry, `source` on any entry
  (`adminguide` or `console-export`; absent means the Developer Guide) and
  the category `other`, five unmodeled types known from Administrator Guide
  block pages or a console export. Nothing in the resource schema changes
  for them; a flow carrying one still plans as a `generic` action.
- Reference scanning follows flow-as-code 78b0a87: a whole-value token that
  follows an unterminated `${cdref:` in an earlier string of the same
  document is now collected and, when nothing binds it, refused at plan
  as unmapped; the regex it replaces skipped over it.
- The vendored `hcl/emit` cases carry `outputs.tf` (flow-as-code C13); the
  conformance run plans it beside `flows.tf` and holds each
  `<name>_document_sha256` output to sha256 of the planned `flowdoc`.

## 0.1.2 (2026-09-30)

Vendors flow-as-code `conformance/` at
`14629631b0030b2bb42a11bc2bf23ec4f303bd23` (the flow-as-code 0.2.1 release
commit) and reads FlowDoc 0.2.

- `compare` needs a `next`. Amazon Connect refuses a
  `compare` action without a `next` ("Action is missing required property.
  Path: Actions[N].Transitions.NextAction", found on 2026-09-30), in every
  flow type probed, as the first action or later, and accepted every target
  tried; the console and flow-as-code's builder use the
  `NoMatchingCondition` branch's target. Plan-time lint gains the
  thirteenth rule, `next-action-required`: an error, not a hard rule, for a
  non-terminal action whose catalog `next` is `required` or `mirrors:*` and
  has none, shown as a plan warning. The refusal was seen on 29 of the
  30 non-terminal types probed, of 31 (rule 38 lists them) and is assumed for
  `connect_participant_with_lex_bot`, the one that was not probed. For
  a mirrored type the message names the branch target to copy.
  `message_participant_iteratively` is not flagged, since the service
  accepts it either way. A `next` on a terminal action, which the service
  refuses, is not checked.
- `hcl/parse/null-attributes` (vendored from flow-as-code `9a2778b`):
  configuration from `-generate-config-out`, with `settings = null` on a flow and `next = null` on a terminal action. The
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
