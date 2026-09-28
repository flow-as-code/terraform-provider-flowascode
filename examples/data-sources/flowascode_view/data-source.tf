# What a flow binds a view:after-contact-work key to.
data "flowascode_view" "after_contact_work" {
  instance_id = var.connect_instance_id
  name        = "after-contact-work"
  type        = "AWS_MANAGED"
}
