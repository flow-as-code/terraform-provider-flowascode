provider "aws" {
  region = var.region

  default_tags {
    tags = local.tags
  }
}

# awscc has no default_tags; every awscc resource below sets local.awscc_tags.
provider "awscc" {
  region = var.region
}
