# D05: an AI agents (Amazon Q in Connect, formerly Wisdom) assistant on an
# AWS-owned key with no knowledge base, associated as WISDOM_ASSISTANT. No
# server_side_encryption_configuration means the AWS-owned key.
# https://docs.aws.amazon.com/connect/latest/adminguide/ai-agent-initial-setup.html
#
# Connect tags resources it associates AmazonConnectEnabled=True and expects
# the tag to stay; a Terraform-managed assistant sets it itself or the next
# apply removes it (same page).
#
# Cost: nothing idle. Under Connect Customer pricing the assistant is included
# in the channel rate; under Customer Basic it is $0.0080 a minute of voice
# when run. Creating a flow runs nothing.

resource "awscc_wisdom_assistant" "sandbox" {
  name        = "${local.prefix}-assistant"
  type        = "AGENT"
  description = "AI agents assistant for the Phase D CreateWisdomSession probes; no knowledge base"

  tags = concat(local.awscc_tags, [
    { key = "AmazonConnectEnabled", value = "True" },
  ])
}

resource "awscc_connect_integration_association" "assistant" {
  instance_id      = local.instance_arn
  integration_arn  = awscc_wisdom_assistant.sandbox.assistant_arn
  integration_type = "WISDOM_ASSISTANT"
}
