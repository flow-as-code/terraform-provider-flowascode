# D07: one predefined attribute for the UpdateRoutingCriteria probes.
# Exercising a match needs a user with a proficiency, which is the showcase's
# concern, not this root's.
# https://docs.aws.amazon.com/connect/latest/adminguide/predefined-attributes.html
#
# The name carries the root's prefix like everything else here: the resource
# takes no tags, so the name is the only mark that it belongs to this root.
# Name is 1 to 100 characters and "Update requires: Replacement":
# https://docs.aws.amazon.com/AWSCloudFormation/latest/TemplateReference/aws-resource-connect-predefinedattribute.html
#
# Cost: free.

resource "awscc_connect_predefined_attribute" "lantern_certified" {
  instance_arn = local.instance_arn
  name         = "${local.prefix}-lantern-certified"

  values = {
    string_list = ["apprentice", "keeper", "master"]
  }
}
