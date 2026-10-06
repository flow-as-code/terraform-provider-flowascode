# D05: a task template for the CreateTask probes (TaskTemplateId must be a
# static value in the action). hashicorp/aws has no task template resource.
# https://docs.aws.amazon.com/connect/latest/adminguide/task-templates.html
#
# Cost: free. A task costs $0.070 when one runs; creating a flow runs none.

resource "awscc_connect_task_template" "probe" {
  instance_arn = local.instance_arn
  name         = "${local.prefix}-task"
  description  = "Task template the Phase D CreateTask probes reference"
  status       = "ACTIVE"

  fields = [
    {
      id   = { name = "Lantern" }
      type = "TEXT"
    },
  ]

  tags = local.awscc_tags
}
