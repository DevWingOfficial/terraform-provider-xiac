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
      source = "XiaCOrg/xiac"
    }
  }
}

variable "xiac_platform_principal_arn" {
  type        = string
  default     = "arn:aws:iam::999999999999:root"
  description = "AWS principal published by XIaC."
}

provider "aws" {}

# Uses XIAC_API_KEY and, optionally, XIAC_ENDPOINT.
provider "xiac" {}

data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}
data "aws_region" "current" {}

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

resource "aws_iam_role" "xiac" {
  name               = "xiac-readonly"
  assume_role_policy = data.aws_iam_policy_document.xiac_assume_role.json
}

resource "aws_iam_role_policy_attachment" "read_only" {
  role       = aws_iam_role.xiac.name
  policy_arn = "arn:${data.aws_partition.current.partition}:iam::aws:policy/ReadOnlyAccess"
}

resource "xiac_aws_account" "this" {
  account_id  = data.aws_caller_identity.current.account_id
  iam_role    = aws_iam_role.xiac.arn
  external_id = random_uuid.xiac_external_id.result
  sts_region  = data.aws_region.current.region
  regions     = []
  readonly    = true

  depends_on = [aws_iam_role_policy_attachment.read_only]
}

output "account_id" {
  value       = xiac_aws_account.this.account_id
  description = "AWS account ID registered with XIaC."
}

output "status" {
  value       = xiac_aws_account.this.status
  description = "Current XIaC connection status."
}
