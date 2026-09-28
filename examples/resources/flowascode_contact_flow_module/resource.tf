resource "flowascode_contact_flow_module" "survey" {
  instance_id = var.connect_instance_id
  name        = "survey"

  action {
    id   = "ask"
    next = "end"
    message_participant {
      text = "One question before you go."
    }
    error {
      type = "NoMatchingError"
      next = "end"
    }
  }

  action {
    id = "end"
    end_flow_module_execution {}
  }
}
