# D03: Customer Profiles. A domain encrypted with a customer managed key, one
# per instance, linked to the instance by a Customer Profiles integration
# whose Uri is the instance ARN.
# https://docs.aws.amazon.com/connect/latest/adminguide/enable-customer-profiles.html
#
# Cost: profiles holding only Connect data are free; the key is about $1 a
# month (https://aws.amazon.com/kms/pricing/). Creating flows uses no profile.

# The console-made key gets the default policy. This one also grants the
# Customer Profiles service principal what the admin guide names for a key it
# does not administer (kms:GenerateDataKey, kms:CreateGrant, kms:Decrypt),
# scoped to this account to prevent the confused deputy:
# https://docs.aws.amazon.com/connect/latest/adminguide/cross-service-confused-deputy-prevention.html
data "aws_iam_policy_document" "profiles_key" {
  statement {
    sid       = "EnableRootAccountAccess"
    actions   = ["kms:*"]
    resources = ["*"]

    principals {
      type        = "AWS"
      identifiers = ["arn:aws:iam::${local.account_id}:root"]
    }
  }

  statement {
    sid = "AllowCustomerProfiles"
    actions = [
      "kms:Decrypt",
      "kms:GenerateDataKey",
      "kms:GenerateDataKeyWithoutPlaintext",
      "kms:CreateGrant",
      "kms:DescribeKey",
    ]
    resources = ["*"]

    principals {
      type        = "Service"
      identifiers = ["profile.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [local.account_id]
    }
  }
}

resource "aws_kms_key" "profiles" {
  description             = "${local.prefix}: Customer Profiles domain key for the Phase D probe sweeps"
  deletion_window_in_days = 7
  enable_key_rotation     = true
  policy                  = data.aws_iam_policy_document.profiles_key.json
}

resource "aws_kms_alias" "profiles" {
  name          = "alias/${local.prefix}-profiles"
  target_key_id = aws_kms_key.profiles.key_id
}

resource "aws_customerprofiles_domain" "sandbox" {
  domain_name             = "${local.prefix}-profiles"
  default_encryption_key  = aws_kms_key.profiles.arn
  default_expiration_days = 366
  tags                    = local.tags
}

# The standard CTR object type, as the console creates it when it links an
# instance. The integration below names it, so it exists first.
resource "awscc_customerprofiles_object_type" "ctr" {
  domain_name      = aws_customerprofiles_domain.sandbox.domain_name
  object_type_name = "CTR"
  description      = "Amazon Connect contact records (standard template)"
  template_id      = "CTR"
  tags             = local.awscc_tags
}

# The link between the instance and the domain. CloudFormation's own example
# for AWS::CustomerProfiles::Integration is exactly this: DomainName,
# ObjectTypeName CTR, Uri the instance ARN.
# https://docs.aws.amazon.com/AWSCloudFormation/latest/TemplateReference/aws-resource-customerprofiles-integration.html
resource "awscc_customerprofiles_integration" "connect" {
  domain_name      = aws_customerprofiles_domain.sandbox.domain_name
  uri              = local.instance_arn
  object_type_name = awscc_customerprofiles_object_type.ctr.object_type_name
  tags             = local.awscc_tags
}
