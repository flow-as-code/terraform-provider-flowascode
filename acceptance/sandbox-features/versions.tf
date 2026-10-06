# Operator-only root: the sandbox-instance features the Phase D probe sweeps
# need (flow-as-code tasks/README.md, Phase D owner decision 7). The
# acceptance lane (.github/workflows/acceptance.yml) never runs it. State is
# local, under state/ (gitignored), and stays on the operator's machine.
terraform {
  required_version = ">= 1.8.0"

  backend "local" {
    path = "state/terraform.tfstate"
  }

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    awscc = {
      source  = "hashicorp/awscc"
      version = "~> 1.104"
    }
    # Packages the message-processor Lambda from lambda/index.mjs at plan time.
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.7"
    }
  }
}
