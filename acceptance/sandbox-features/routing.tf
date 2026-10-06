# D07: one predefined attribute for the UpdateRoutingCriteria probes.
# Exercising a match needs a user with a proficiency, which is the showcase's
# concern, not this root's.
# https://docs.aws.amazon.com/connect/latest/adminguide/predefined-attributes.html
#
# Cost: free.

resource "awscc_connect_predefined_attribute" "lantern_certified" {
  instance_arn = local.instance_arn
  name         = "lantern-certified"

  values = {
    string_list = ["apprentice", "keeper", "master"]
  }
}
