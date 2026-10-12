# D05: a task template for the CreateTask probes (TaskTemplateId must be a
# static value in the action). hashicorp/aws has no task template resource.
# https://docs.aws.amazon.com/connect/latest/adminguide/task-templates.html
#
# CreateTaskTemplate requires exactly one field of type NAME, listed in
# Constraints.RequiredFields (the service requires Constraints although the
# model marks it optional), and either a ContactFlowId or a field of type
# QUICK_CONNECT. This template takes the QUICK_CONNECT field, so it depends on
# no flow:
# https://docs.aws.amazon.com/connect/latest/APIReference/API_CreateTaskTemplate.html
# https://docs.aws.amazon.com/AWSCloudFormation/latest/TemplateReference/aws-resource-connect-tasktemplate.html
#
# Cost: free. A task costs $0.070 when one runs; creating a flow runs none.

resource "awscc_connect_task_template" "probe" {
  instance_arn = local.instance_arn
  name         = "${local.prefix}-task"
  description  = "Task template the Phase D CreateTask probes reference"
  status       = "ACTIVE"

  fields = [
    {
      id   = { name = "Name" }
      type = "NAME"
    },
    {
      id   = { name = "QuickConnect" }
      type = "QUICK_CONNECT"
    },
    {
      id   = { name = "Lantern" }
      type = "TEXT"
    },
  ]

  constraints = {
    required_fields = [
      { id = { name = "Name" } },
    ]
  }

  tags = local.awscc_tags
}
