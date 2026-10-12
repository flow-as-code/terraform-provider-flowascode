# D04 and D05: a flow and a queue for the probes that reference one
# (CreateTask.ContactFlowId, StartOutboundChatContact.ContactFlowArn, and the
# {{QUEUE_ID}} of flow-as-code conformance/flow-language/probes/README.md).
#
# The flow is created here rather than looked up, so its name carries the
# root's prefix and no default flow's name is assumed. It disconnects and
# does nothing else; the shape is the flow language example's:
# https://docs.aws.amazon.com/connect/latest/APIReference/flow-language-example.html
#
# Cost: free; nothing runs it.

resource "aws_connect_contact_flow" "probe_target" {
  instance_id = var.instance_id
  name        = "${local.prefix}-probe-target"
  description = "Disconnects; the flow the Phase D probes reference"
  type        = "CONTACT_FLOW"

  content = jsonencode({
    Version     = "2019-10-30"
    StartAction = "disconnect"
    Actions = [
      {
        Identifier  = "disconnect"
        Type        = "DisconnectParticipant"
        Parameters  = {}
        Transitions = {}
      },
    ]
  })
}

# Every instance has a default queue named BasicQueue:
# https://docs.aws.amazon.com/connect/latest/adminguide/concepts-queues-standard-and-agent.html#concepts-default-queue
data "aws_connect_queue" "basic" {
  instance_id = var.instance_id
  name        = "BasicQueue"
}
