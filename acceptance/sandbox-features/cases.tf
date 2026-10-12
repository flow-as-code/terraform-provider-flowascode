# D06: Cases. A domain associated to the instance (CASES_DOMAIN), one text
# field, one single-select field and one template, so CreateCase, GetCase and
# UpdateCase probes have ids to reference. Cases needs Customer Profiles on
# the instance first, hence the depends_on.
# https://docs.aws.amazon.com/connect/latest/adminguide/enable-cases.html
#
# Cost: $0.12 per case created; nothing idle. Creating a flow creates no case.
# https://aws.amazon.com/products/connect/customer/pricing/appendix/

resource "awscc_cases_domain" "sandbox" {
  name = "${local.prefix}-cases"
  tags = local.awscc_tags
}

# IntegrationType CASES_DOMAIN is in the CloudFormation allowed values. Every
# property but Tags is "Update requires: Replacement"; Tags updates in place:
# https://docs.aws.amazon.com/AWSCloudFormation/latest/TemplateReference/aws-resource-connect-integrationassociation.html
resource "awscc_connect_integration_association" "cases" {
  instance_id      = local.instance_arn
  integration_arn  = awscc_cases_domain.sandbox.domain_arn
  integration_type = "CASES_DOMAIN"
  tags             = local.awscc_tags

  depends_on = [awscc_customerprofiles_integration.connect]
}

# "title" is a system field; its id is the literal `title` and it is never
# created: https://docs.aws.amazon.com/connect/latest/adminguide/case-fields.html
resource "awscc_cases_field" "notes" {
  domain_id   = awscc_cases_domain.sandbox.domain_id
  name        = "${local.prefix}-notes"
  type        = "Text"
  description = "Free text, for the UpdateCase probe"
  tags        = local.awscc_tags
}

resource "awscc_cases_field" "lantern" {
  domain_id   = awscc_cases_domain.sandbox.domain_id
  name        = "${local.prefix}-lantern"
  type        = "SingleSelect"
  description = "Single select, for the CreateCase probe"
  tags        = local.awscc_tags
}

# Single-select options are not a CloudFormation property; the Cases API sets
# them with BatchPutFieldOptions, which creates and updates (so a re-run is
# harmless). The aws CLI must be on PATH with the same credentials as the
# providers. The README gives the same call to run by hand.
# https://docs.aws.amazon.com/connect/latest/APIReference/API_connect-cases_BatchPutFieldOptions.html
# The CLI shorthand members are lower case (name, value, active); capitalised
# keys fail the CLI's parameter validation before any request is sent:
# https://docs.aws.amazon.com/cli/latest/reference/connectcases/batch-put-field-options.html
locals {
  lantern_options = [
    { name = "Unlit", value = "unlit" },
    { name = "Lit", value = "lit" },
    { name = "Guttering", value = "guttering" },
  ]
  lantern_options_cli = join(" ", [
    for o in local.lantern_options : "name=${o.name},value=${o.value},active=true"
  ])
}

resource "terraform_data" "lantern_options" {
  triggers_replace = [awscc_cases_field.lantern.field_id, local.lantern_options_cli]

  provisioner "local-exec" {
    command = "aws connectcases batch-put-field-options --region ${var.region} --domain-id ${awscc_cases_domain.sandbox.domain_id} --field-id ${awscc_cases_field.lantern.field_id} --options ${local.lantern_options_cli}"
  }
}

resource "awscc_cases_template" "probe" {
  domain_id   = awscc_cases_domain.sandbox.domain_id
  name        = "${local.prefix}-probe"
  description = "Template the Phase D Cases probes reference"
  status      = "Active"

  required_fields = [
    { field_id = awscc_cases_field.notes.field_id },
  ]

  tags = local.awscc_tags

  depends_on = [terraform_data.lantern_options]
}
