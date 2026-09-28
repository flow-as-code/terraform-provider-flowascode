resource "flowascode_contact_flow" "appointment_line" {
  instance_id = var.connect_instance_id
  name        = "appointment-line"
  type        = "CONTACT_FLOW"
  description = "Main appointment line"

  # One entry per reference the actions use: the key they hold, bound to
  # the ARN as a Terraform expression. Never a literal ARN in an action.
  refs = {
    "hours:main-line"    = aws_connect_hours_of_operation.main_line.arn
    "queue:appointments" = aws_connect_queue.appointments.arn
  }

  action {
    id   = "welcome"
    next = "check-hours"
    message_participant {
      text = "Thanks for calling."
    }
    error {
      type = "NoMatchingError"
      next = "hang-up"
    }
  }

  action {
    id   = "check-hours"
    next = "hang-up"
    check_hours_of_operation {
      hours_of_operation_id = "hours:main-line"
    }
    condition {
      operator = "Equals"
      operands = ["True"]
      next     = "to-queue"
    }
    condition {
      operator = "Equals"
      operands = ["False"]
      next     = "hang-up"
    }
    error {
      type = "NoMatchingError"
      next = "hang-up"
    }
  }

  action {
    id   = "to-queue"
    next = "transfer"
    update_contact_target_queue {
      queue_id = "queue:appointments"
    }
    error {
      type = "NoMatchingError"
      next = "hang-up"
    }
  }

  action {
    id = "transfer"
    transfer_contact_to_queue {}
    error {
      type = "QueueAtCapacity"
      next = "hang-up"
    }
    error {
      type = "NoMatchingError"
      next = "hang-up"
    }
  }

  action {
    id = "hang-up"
    disconnect_participant {}
  }
}
