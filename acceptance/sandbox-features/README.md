# Sandbox features for the Phase D probe sweeps

An operator-only OpenTofu root that puts on the shared sandbox Amazon Connect
instance the features flow-as-code's Phase D probe sweeps need
(flow-as-code `tasks/README.md`, Phase D, owner decision 7, and the "Sandbox
prerequisites and cost" section of each of D02 to D09). The acceptance lane
(`.github/workflows/acceptance.yml`) never runs it. Its state is local, under
`state/` (gitignored), and stays on the operator's machine; nothing here is
shared through CI.

Everything it creates is named `fac-sbx-*` and tagged `Project=flow-as-code`,
`Purpose=phase-d-probes`, `Root=acceptance/sandbox-features`. Nothing costs
money while idle except the Customer Profiles KMS key (about $1 a month) and
the claimed phone number (about $0.90 a month). Creating a flow that names
any of these runs nothing and bills nothing.

## What each feature is for

| Resource (names)                                                                   | Task     | Why the probes need it                                                                                                                      | Cost when used                                                                                   |
| ---------------------------------------------------------------------------------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| Customer Profiles domain `fac-sbx-profiles`, its customer managed key, the standard CTR object type and the integration whose Uri is the instance ARN | D03      | The six Customer Profiles actions are refused on an instance with no domain; the integration is what links instance and domain             | Profiles holding only Connect data are free; the key about $1 a month                            |
| Cases domain `fac-sbx-cases`, `CASES_DOMAIN` association, fields `fac-sbx-notes` (text) and `fac-sbx-lantern` (single select), template `fac-sbx-probe` | D06      | `CreateCase`, `GetCase` and `UpdateCase` name a template id and per-domain field ids; the system field `title` needs no creation           | $0.12 per case created; a flow create creates none                                               |
| AI agents assistant `fac-sbx-assistant` (AWS-owned key, no knowledge base) and its `WISDOM_ASSISTANT` association | D05      | `CreateWisdomSession` names the assistant; the `AmazonConnectEnabled=True` tag is set here so an apply never strips what Connect expects   | Included under Connect Customer pricing; $0.0080 a minute of voice under Customer Basic          |
| Task template `fac-sbx-task`                                                       | D05      | `CreateTask` carries a static `TaskTemplateId`                                                                                              | $0.070 per task run                                                                              |
| Predefined attribute `lantern-certified` (`apprentice`, `keeper`, `master`)        | D07      | `UpdateRoutingCriteria` names a predefined attribute                                                                                        | Free                                                                                             |
| Live media streaming, prefix `fac-sbx-media`, no data retention, `aws/kinesisvideo` key | D02      | `UpdateContactMediaStreamingBehavior` is refused on an instance without media streaming                                                     | $0.0085 per GB ingested; nothing stored                                                          |
| Lambda `fac-sbx-message-processor`, associated plainly and as `MESSAGE_PROCESSOR` | D02      | `UpdateContactMediaProcessing` names a message processor; the plain association gives an `InvokeLambdaFunction` probe a function to name   | Invocations only                                                                                 |
| One US DID, kept                                                                   | D04, D09 | `CompleteOutboundCall` and `TransferParticipantToThirdParty` take a caller id; inbound test calls need a number                             | About $0.03 a day, $0.0022 a minute inbound                                                      |

Not created, by owner decision 6 and the Voice ID end of support: outbound
campaigns (a quota ticket), SMS (a registration taking up to 15 business
days), and a Voice ID domain (the service ended on 2026-05-20).

Each `.tf` file carries the AWS documentation URL the resource's shape
relies on.

## Running it

Credentials and the instance id are never written to a tracked file. The
instance is the one whose alias starts `flow-as-code-sbx`:

```
export AWS_PROFILE=flow-as-code AWS_REGION=us-west-2
aws connect list-instances --query 'InstanceSummaryList[?starts_with(InstanceAlias, `flow-as-code-sbx`)].Id' --output text
```

Then, from this directory, with the id on the command line (or in a
gitignored `*.tfvars`):

```
tofu init
tofu plan -var instance_id=<id>
tofu apply -var instance_id=<id>
```

`tofu init` (without `-backend=false`) creates `state/`. The lock file is
tracked for darwin and linux on amd64 and arm64, so every operator gets the
same provider builds. Offline, `tofu init -backend=false`, `tofu validate`
and `tofu fmt -check` are what CI could run; none needs credentials.

Three things to know before the first apply:

- The Cases single-select options are not a CloudFormation property. The
  `terraform_data.lantern_options` resource sets them with the aws CLI
  (`aws connectcases batch-put-field-options`), so the CLI must be on `PATH`
  with the same credentials. The same call by hand, should it ever be needed:

  ```
  aws connectcases batch-put-field-options --domain-id <cases_domain_id> --field-id <cases_field_select_id> \
    --options Name=Unlit,Value=unlit,Active=true Name=Lit,Value=lit,Active=true Name=Guttering,Value=guttering,Active=true
  ```

- The media streaming key is the AWS managed key `alias/aws/kinesisvideo`.
  Reading it creates it on an account that has never used Kinesis Video
  Streams (DescribeKey on a predefined AWS alias does that), so the data
  source succeeds on first use.
- The phone number is claimed at apply. `prefix` is optional; set
  `-var phone_number_prefix=+1512` (any E.164 prefix) to steer the choice.

## Reading the outputs into the probe runner

Every output is an id or an ARN the probes reference. None is marked
sensitive, so `tofu output` prints them, and none may be committed anywhere:
not in a probe input, a results file, a task file or a commit message. The
runner (flow-as-code `scripts/probe-create.mjs`) fills `{{NAME}}` in a probe
from `PROBE_NAME` and scrubs every filled value out of what it records
(`conformance/flow-language/probes/README.md`). Each output's name, upper
cased, is the placeholder it fills, so one line exports them all:

```
eval "$(tofu output -json | jq -r 'to_entries[] | select(.value.value | type == "string") | "export PROBE_\(.key | ascii_upcase)=\(.value.value | @sh)"')"
export CONNECT_INSTANCE_ID=<id> AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
```

So a probe reads `{{CASES_TEMPLATE_ID}}` where it needs the template,
`{{ASSISTANT_ARN}}` for the assistant, `{{TASK_TEMPLATE_ID}}`,
`{{PREDEFINED_ATTRIBUTE_NAME}}`, `{{LAMBDA_ARN}}`, `{{PHONE_NUMBER_ARN}}`,
`{{CASES_FIELD_TEXT_ID}}`, `{{CASES_FIELD_SELECT_ID}}` and so on; the three
well-known placeholders (`{{ACCOUNT}}`, `{{REGION}}`, `{{INSTANCE}}`) come
from `AWS_ACCOUNT_ID`, `AWS_REGION` and `CONNECT_INSTANCE_ID`. The list
outputs (`cases_field_select_values`, `predefined_attribute_values`) are for
reading, not placeholders. `tofu output -json > outputs.json` is gitignored
if anyone does it; do not move that file elsewhere.

## Teardown order

Owner decision 7 keeps the features between tasks and tears them down at
D10, except the phone number, which is kept and never released: releasing it
starts a cooldown of up to 180 days before it can be claimed again, and
claim-and-release cycles beyond 200% of the quota block further claims
(https://docs.aws.amazon.com/connect/latest/APIReference/API_ReleasePhoneNumber.html).
`prevent_destroy` on the number makes `tofu destroy` refuse while it is in
state, so:

1. `tofu state rm aws_connect_phone_number.did` leaves the number claimed to
   the instance and out of this root's reach.
2. `tofu destroy -var instance_id=<id>` removes the rest in dependency
   order: the three integration associations (`MESSAGE_PROCESSOR`,
   `WISDOM_ASSISTANT`, `CASES_DOMAIN`), then the Lambda, its permission and
   role, the assistant, the task template, the predefined attribute, the
   media streaming storage config, the Cases template, fields and domain,
   then the Customer Profiles integration, object type and domain, and last
   the KMS key, which enters its 7 day deletion window.
3. Check nothing named `fac-sbx-*` remains: `aws connect
   list-integration-associations --instance-id <id>`, `aws customer-profiles
   list-domains`, `aws connectcases list-domains`, `aws qconnect
   list-assistants`, `aws lambda list-functions`.

A Customer Profiles domain with data refuses deletion until its objects are
gone; the probes create no profiles, so that does not arise unless something
else used the domain. If `tofu destroy` is interrupted, re-run it; every
resource here is safe to delete twice.
