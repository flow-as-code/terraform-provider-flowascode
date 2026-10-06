# D04 and D09: one US DID claimed to the instance, kept and never released
# (owner decision 7). Releasing a number starts a cooldown of up to 180 days
# before it can be claimed again, and claim-and-release cycles beyond 200% of
# the phone number quota block further claims:
# https://docs.aws.amazon.com/connect/latest/APIReference/API_ReleasePhoneNumber.html
# prevent_destroy makes `tofu destroy` refuse while this resource is in state;
# the README's teardown removes it from state first so the number stays claimed.
#
# Cost: about $0.03 a day ($0.90 a month) for a US DID, $0.0022 a minute
# inbound. Nothing else idle.

resource "aws_connect_phone_number" "did" {
  target_arn   = local.instance_arn
  country_code = "US"
  type         = "DID"
  prefix       = var.phone_number_prefix
  description  = "${local.prefix}: kept US DID for the Phase D probes; never release"

  lifecycle {
    prevent_destroy = true
  }
}
