terraform {
  required_providers {
    flowascode = {
      source  = "flow-as-code/flowascode"
      version = ">= 0.1"
    }
  }
}

# Authentication is hashicorp/aws's: the same attributes, environment
# variables and shared files, so an existing AWS setup carries over.
provider "flowascode" {
  region = "us-west-2"
}
