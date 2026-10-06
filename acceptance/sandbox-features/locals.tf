locals {
  prefix = "fac-sbx"

  tags = {
    Project   = "flow-as-code"
    Purpose   = "phase-d-probes"
    Root      = "acceptance/sandbox-features"
    ManagedBy = "opentofu"
  }

  # awscc resources take tags as a list of {key, value}.
  awscc_tags = [for k, v in local.tags : { key = k, value = v }]

  account_id   = data.aws_caller_identity.current.account_id
  instance_arn = data.aws_connect_instance.sandbox.arn
}
