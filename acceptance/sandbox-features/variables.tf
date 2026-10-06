variable "instance_id" {
  description = "The id of the shared sandbox Amazon Connect instance (alias flow-as-code-sbx-*). Read it with `aws connect list-instances`; pass it on the command line or in a gitignored *.tfvars file, never in a tracked file."
  type        = string

  validation {
    condition     = can(regex("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", var.instance_id))
    error_message = "instance_id must be the instance's UUID, not its ARN or alias."
  }
}

variable "region" {
  description = "The Region the sandbox instance lives in."
  type        = string
  default     = "us-west-2"
}

variable "phone_number_prefix" {
  description = "Optional E.164 prefix for the claimed US DID, for example +1512. Null lets the service choose any US DID."
  type        = string
  default     = null
}
