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

output "cases_field_title_id" {
  description = "D06: the system title field's id; a constant, never created."
  value       = "title"
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
