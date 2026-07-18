terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 6.0"
    }
    random = {
      source = "hashicorp/random"
    }
    xiac = {
      source = "DevWingOfficial/xiac"
    }
  }
}

variable "xiac_api_key" {
  type        = string
  sensitive   = true
  description = "Tenant API key for the XIaC platform (X-Api-Key)."
}

variable "xiac_platform_principal_arn" {
  type        = string
  description = "AWS principal ARN XIaC publishes for cross-account discovery."
}

variable "provider_id" {
  type        = string
  default     = null
  description = "Existing XIaC provider id from app onboarding. Leave null for standalone creation."
}

variable "regions" {
  type        = list(string)
  description = "AWS regions XIaC may scan. Empty lets XIaC resolve enabled regions."
  default     = []
}

variable "sts_region" {
  type        = string
  default     = ""
  description = "AWS region for STS assume role. Empty inherits the configured AWS provider region."
}

provider "xiac" {
  api_key  = var.xiac_api_key
  endpoint = "http://127.0.0.1:8099"
}

data "aws_region" "current" {}

locals {
  sts_region = trimspace(var.sts_region) != "" ? var.sts_region : data.aws_region.current.region
}

# The client owns this trust anchor. The same UUID is sent to XIaC and placed
# in the AWS role trust policy, so there is no two-phase apply.
resource "random_uuid" "xiac_external_id" {}

data "aws_iam_policy_document" "xiac_assume_role" {
  statement {
    actions = ["sts:AssumeRole"]
    effect  = "Allow"

    principals {
      type        = "AWS"
      identifiers = [var.xiac_platform_principal_arn]
    }

    condition {
      test     = "StringEquals"
      variable = "sts:ExternalId"
      values   = [random_uuid.xiac_external_id.result]
    }
  }
}

resource "aws_iam_role" "xiac_role" {
  name               = "xiac-readonly"
  assume_role_policy = data.aws_iam_policy_document.xiac_assume_role.json
}

resource "xiac_aws_account" "demo" {
  provider_id = var.provider_id
  iam_role    = aws_iam_role.xiac_role.arn
  external_id = random_uuid.xiac_external_id.result
  regions     = var.regions
  sts_region  = local.sts_region
  readonly    = true
}

output "external_id" {
  description = "Client-generated External ID registered with XIaC."
  value       = random_uuid.xiac_external_id.result
}

output "account_id" {
  description = "AWS account id discovered by XIaC."
  value       = xiac_aws_account.demo.account_id
}
