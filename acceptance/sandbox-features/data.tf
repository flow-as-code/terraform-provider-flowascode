data "aws_caller_identity" "current" {}

# Resolves the instance ARN from the id and fails at plan time if the id is
# not an instance in this account and Region.
data "aws_connect_instance" "sandbox" {
  instance_id = var.instance_id
}

# The AWS managed key for Kinesis Video Streams. DescribeKey on a predefined
# AWS alias creates the AWS managed key if it does not exist yet, so this read
# succeeds on an account that has never used Kinesis Video Streams:
# https://docs.aws.amazon.com/kms/latest/APIReference/API_DescribeKey.html
data "aws_kms_key" "kinesisvideo" {
  key_id = "alias/aws/kinesisvideo"
}
