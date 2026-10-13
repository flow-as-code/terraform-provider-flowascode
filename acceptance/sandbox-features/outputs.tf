# Every id and ARN the probes need. None is marked sensitive, so `tofu output`
# prints them, and none may be committed anywhere: the probe runner fills
# `{{NAME}}` from `PROBE_NAME` at run time and scrubs filled values out of
# what it records (flow-as-code conformance/flow-language/probes/README.md).
# Each output's name upper-cased is the placeholder it fills.

output "instance_arn" {
  description = "The sandbox instance ARN, resolved from var.instance_id."
  value       = local.instance_arn
  sensitive   = false
}

output "flow_id" {
  description = "D04 and D05: the probe target flow's id."
  value       = aws_connect_contact_flow.probe_target.contact_flow_id
  sensitive   = false
}

output "flow_arn" {
  description = "D04 and D05: the probe target flow's ARN."
  value       = aws_connect_contact_flow.probe_target.arn
  sensitive   = false
}

output "queue_id" {
  description = "The instance's default BasicQueue id."
  value       = data.aws_connect_queue.basic.queue_id
  sensitive   = false
}

output "queue_arn" {
  description = "The instance's default BasicQueue ARN."
  value       = data.aws_connect_queue.basic.arn
  sensitive   = false
}

output "profiles_domain_name" {
  description = "D03: the Customer Profiles domain name."
  value       = aws_customerprofiles_domain.sandbox.domain_name
  sensitive   = false
}

output "profiles_domain_arn" {
  description = "D03: the Customer Profiles domain ARN."
  value       = aws_customerprofiles_domain.sandbox.arn
  sensitive   = false
}

output "profiles_kms_key_arn" {
  description = "D03: the customer managed key the domain is encrypted with."
  value       = aws_kms_key.profiles.arn
  sensitive   = false
}

output "cases_domain_id" {
  description = "D06: the Cases domain id."
  value       = awscc_cases_domain.sandbox.domain_id
  sensitive   = false
}

output "cases_domain_arn" {
  description = "D06: the Cases domain ARN."
  value       = awscc_cases_domain.sandbox.domain_arn
  sensitive   = false
}

output "cases_integration_association_id" {
  description = "D06: the CASES_DOMAIN integration association id."
  value       = awscc_connect_integration_association.cases.integration_association_id
  sensitive   = false
}

output "cases_template_id" {
  description = "D06: the template id a CreateCase probe names."
  value       = awscc_cases_template.probe.template_id
  sensitive   = false
}

output "cases_template_arn" {
  description = "D06: the template ARN."
  value       = awscc_cases_template.probe.template_arn
  sensitive   = false
}

output "cases_field_text_id" {
  description = "D06: the custom text field's id (a per-domain UUID)."
  value       = awscc_cases_field.notes.field_id
  sensitive   = false
}

output "cases_field_select_id" {
  description = "D06: the custom single-select field's id (a per-domain UUID)."
  value       = awscc_cases_field.lantern.field_id
  sensitive   = false
}

output "cases_field_select_values" {
  description = "D06: the single-select field's option values."
  value       = [for o in local.lantern_options : o.value]
  sensitive   = false
}

output "assistant_id" {
  description = "D05: the AI agents assistant id."
  value       = awscc_wisdom_assistant.sandbox.assistant_id
  sensitive   = false
}

output "assistant_arn" {
  description = "D05: the AI agents assistant ARN a CreateWisdomSession probe names."
  value       = awscc_wisdom_assistant.sandbox.assistant_arn
  sensitive   = false
}

output "assistant_integration_association_id" {
  description = "D05: the WISDOM_ASSISTANT integration association id."
  value       = awscc_connect_integration_association.assistant.integration_association_id
  sensitive   = false
}

output "task_template_arn" {
  description = "D05: the task template ARN."
  value       = awscc_connect_task_template.probe.arn
  sensitive   = false
}

output "task_template_id" {
  description = "D05: the task template id (the last segment of its ARN), the static TaskTemplateId a CreateTask probe carries."
  value       = element(split("/", awscc_connect_task_template.probe.arn), length(split("/", awscc_connect_task_template.probe.arn)) - 1)
  sensitive   = false
}

output "predefined_attribute_name" {
  description = "D07: the predefined attribute name an UpdateRoutingCriteria probe names."
  value       = awscc_connect_predefined_attribute.lantern_certified.name
  sensitive   = false
}

output "predefined_attribute_values" {
  description = "D07: the predefined attribute's values."
  value       = awscc_connect_predefined_attribute.lantern_certified.values.string_list
  sensitive   = false
}

output "media_streams_association_id" {
  description = "D02: the MEDIA_STREAMS storage config association id."
  value       = aws_connect_instance_storage_config.media_streams.association_id
  sensitive   = false
}

output "media_streams_prefix" {
  description = "D02: the Kinesis Video Streams prefix."
  value       = "${local.prefix}-media"
  sensitive   = false
}

output "lambda_arn" {
  description = "D02: the Lambda ARN, associated to the instance both plainly and as a MESSAGE_PROCESSOR."
  value       = aws_lambda_function.message_processor.arn
  sensitive   = false
}

output "plain_lambda_arn" {
  description = "D02: a Lambda ARN associated to the instance plainly and never as a MESSAGE_PROCESSOR."
  value       = aws_lambda_function.plain.arn
  sensitive   = false
}

output "lambda_name" {
  description = "D02: the Lambda function name."
  value       = aws_lambda_function.message_processor.function_name
  sensitive   = false
}

output "message_processor_integration_association_id" {
  description = "D02: the MESSAGE_PROCESSOR integration association id."
  value       = awscc_connect_integration_association.message_processor.integration_association_id
  sensitive   = false
}

output "phone_number_id" {
  description = "D04 and D09: the claimed US DID's id."
  value       = aws_connect_phone_number.did.id
  sensitive   = false
}

output "phone_number_arn" {
  description = "D04 and D09: the claimed US DID's ARN, for a caller id."
  value       = aws_connect_phone_number.did.arn
  sensitive   = false
}

output "phone_number" {
  description = "D04 and D09: the claimed US DID in E.164 form."
  value       = aws_connect_phone_number.did.phone_number
  sensitive   = false
}

# ---------------------------------------------------------------------------
# Probe placeholders. Each output below is named for the placeholder a probe
# set under flow-as-code conformance/flow-language/probes/<rule>/ uses, so
# the README's one-line export fills every set with no mapping. Some repeat
# an output above under the set's name. Not here, and why (the README's "Not
# created yet" list): {{AGENT_ID}} (rule 45; a user of the instance, read
# with `aws connect list-users`), {{PROFILE_ID}} and
# {{CALCULATED_ATTRIBUTE}} (rule 43), {{ENCRYPTION_KEY_ID}} and
# {{ENCRYPTION_KEY}} (rule 47).

# A "missing" id must look like a real one and name nothing. These are
# derived from md5 of a fixed string, so they are UUID-shaped, stable across
# plans, and need no provider and no state.
locals {
  missing_ids = {
    for k in ["task-template", "case-field", "case-template"] : k => join("-", [
      substr(md5("${local.prefix}-missing-${k}"), 0, 8),
      substr(md5("${local.prefix}-missing-${k}"), 8, 4),
      substr(md5("${local.prefix}-missing-${k}"), 12, 4),
      substr(md5("${local.prefix}-missing-${k}"), 16, 4),
      substr(md5("${local.prefix}-missing-${k}"), 20, 12),
    ])
  }
}

# Rule 41 (D02).
output "message_processor_name" {
  description = "Rule 41: a Lambda the instance has associated as MESSAGE_PROCESSOR, by name."
  value       = aws_lambda_function.message_processor.function_name
  sensitive   = false
}

output "unassociated_lambda_name" {
  description = "Rule 41: a Lambda in the same account and Region the instance has never associated, by name."
  value       = aws_lambda_function.unassociated.function_name
  sensitive   = false
}

# Rule 42 (D05). task_template_id and assistant_id are above.
output "task_flow_id" {
  description = "Rule 42: a published contact flow, the flow a task runs (the probe target flow)."
  value       = aws_connect_contact_flow.probe_target.contact_flow_id
  sensitive   = false
}

output "missing_task_template_id" {
  description = "Rule 42: a UUID that names no task template on the instance."
  value       = local.missing_ids["task-template"]
  sensitive   = false
}

output "unlinked_assistant_id" {
  description = "Rule 42: an assistant in the same account and Region the instance has not linked."
  value       = awscc_wisdom_assistant.unlinked.assistant_id
  sensitive   = false
}

# Rule 43 (D03).
output "object_type" {
  description = "Rule 43: the domain's object type, by name."
  value       = awscc_customerprofiles_object_type.ctr.object_type_name
  sensitive   = false
}

output "object_identifier" {
  description = "Rule 43: a search key of that object type: _ctrContactId, a key of the standard CTR template (GetProfileObjectTypeTemplate, TemplateId CTR)."
  value       = "_ctrContactId"
  sensitive   = false
}

# Rule 44 (D06). cases_domain_id is above.
output "case_template_id" {
  description = "Rule 44: the Cases template id."
  value       = awscc_cases_template.probe.template_id
  sensitive   = false
}

output "case_text_field_id" {
  description = "Rule 44: the custom text field's id."
  value       = awscc_cases_field.notes.field_id
  sensitive   = false
}

output "case_select_field_id" {
  description = "Rule 44: the custom single-select field's id."
  value       = awscc_cases_field.lantern.field_id
  sensitive   = false
}

output "case_select_option" {
  description = "Rule 44: one option value of the single-select field."
  value       = local.lantern_options[0].value
  sensitive   = false
}

output "missing_case_field_id" {
  description = "Rule 44: a UUID that names no field in the Cases domain."
  value       = local.missing_ids["case-field"]
  sensitive   = false
}

output "missing_case_template_id" {
  description = "Rule 44: a UUID that names no template in the Cases domain."
  value       = local.missing_ids["case-template"]
  sensitive   = false
}

# Rule 45 (D07).
output "predefined_attribute" {
  description = "Rule 45: the predefined attribute, by name."
  value       = awscc_connect_predefined_attribute.lantern_certified.name
  sensitive   = false
}

output "predefined_value" {
  description = "Rule 45: one value the predefined attribute defines."
  value       = awscc_connect_predefined_attribute.lantern_certified.values.string_list[0]
  sensitive   = false
}

# Rule 46 (D04). flow_id is above.
output "did_number" {
  description = "Rule 46: the claimed DID in E.164 form."
  value       = aws_connect_phone_number.did.phone_number
  sensitive   = false
}

output "did_id" {
  description = "Rule 46: the claimed DID's phone number id."
  value       = aws_connect_phone_number.did.id
  sensitive   = false
}
