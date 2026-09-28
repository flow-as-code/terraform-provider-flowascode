# A snapshot of the module, replaced whenever its content changes. Create the
# new version before destroying the old, so an alias always has one to point at.
resource "flowascode_contact_flow_module_version" "survey" {
  instance_id            = var.connect_instance_id
  contact_flow_module_id = flowascode_contact_flow_module.survey.contact_flow_module_id
  content_hash           = flowascode_contact_flow_module.survey.content_hash
  description            = "Survey as reviewed"

  lifecycle {
    create_before_destroy = true
  }
}
