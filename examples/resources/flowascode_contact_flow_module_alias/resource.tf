# What a flow binds a module:survey@prod key to.
resource "flowascode_contact_flow_module_alias" "survey_prod" {
  instance_id                 = var.connect_instance_id
  contact_flow_module_id      = flowascode_contact_flow_module.survey.contact_flow_module_id
  name                        = "prod"
  contact_flow_module_version = flowascode_contact_flow_module_version.survey.version
  description                 = "The version production invokes"
}
