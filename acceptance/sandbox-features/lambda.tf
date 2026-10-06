# D02: a small Lambda associated twice: as a MESSAGE_PROCESSOR, for the
# UpdateContactMediaProcessing probes, and as a plain Lambda association, so
# an InvokeLambdaFunction probe can name a function the instance may call.
# The handler implements the custom message processor contract and returns
# every message approved unchanged.
# https://docs.aws.amazon.com/connect/latest/adminguide/redaction-message-processing.html
#
# Cost: invocations only; none while idle.

data "archive_file" "message_processor" {
  type        = "zip"
  source_file = "${path.module}/lambda/index.mjs"
  output_path = "${path.module}/build/message-processor.zip"
}

data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "message_processor" {
  name               = "${local.prefix}-message-processor"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "message_processor_logs" {
  role       = aws_iam_role.message_processor.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_lambda_function" "message_processor" {
  function_name    = "${local.prefix}-message-processor"
  description      = "Approves every chat message unchanged; a MESSAGE_PROCESSOR for the Phase D probes"
  role             = aws_iam_role.message_processor.arn
  runtime          = "nodejs22.x"
  handler          = "index.handler"
  architectures    = ["arm64"]
  filename         = data.archive_file.message_processor.output_path
  source_code_hash = data.archive_file.message_processor.output_base64sha256
  # The message processor timeout must be between 3 seconds and 3 minutes.
  timeout     = 3
  memory_size = 128
}

# Connect invokes a message processor directly; the plain association below
# grants the instance's own invoke permission, this statement covers the
# processor path and is scoped to this account.
resource "aws_lambda_permission" "connect" {
  statement_id   = "${local.prefix}-connect-invoke"
  action         = "lambda:InvokeFunction"
  function_name  = aws_lambda_function.message_processor.function_name
  principal      = "connect.amazonaws.com"
  source_account = local.account_id
}

# The plain association ("Grant Connect access to your Lambda functions").
# https://docs.aws.amazon.com/connect/latest/adminguide/connect-lambda-functions.html
resource "aws_connect_lambda_function_association" "message_processor" {
  instance_id  = var.instance_id
  function_arn = aws_lambda_function.message_processor.arn
}

# IntegrationType MESSAGE_PROCESSOR is in the CloudFormation allowed values:
# https://docs.aws.amazon.com/AWSCloudFormation/latest/TemplateReference/aws-resource-connect-integrationassociation.html
resource "awscc_connect_integration_association" "message_processor" {
  instance_id      = local.instance_arn
  integration_arn  = aws_lambda_function.message_processor.arn
  integration_type = "MESSAGE_PROCESSOR"

  depends_on = [aws_lambda_permission.connect]
}
