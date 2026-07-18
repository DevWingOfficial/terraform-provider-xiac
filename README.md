# terraform-provider-xiac

Declare an XIaC cloud account connection as Infrastructure-as-Code. The provider
talks to the XIaC platform API and manages the lifecycle of a provider
connection (create → connect/verify → delete) as a Terraform resource.

Built on the current [terraform-plugin-framework](https://github.com/hashicorp/terraform-plugin-framework)
v1 API.

## Installation

The public source repository is ready at
[`DevWingOfficial/terraform-provider-xiac`](https://github.com/DevWingOfficial/terraform-provider-xiac).
After the first signed provider release is published, configure the provider as:

```hcl
terraform {
  required_providers {
    xiac = {
      source  = "DevWingOfficial/xiac"
      version = ">= 1.0"
    }
  }
}
```

The repository source is public, but source publication alone does not make a
provider version installable. Do not select `>= 1.0` until the corresponding
signed release is available from the Terraform/OpenTofu registry.

## Provider configuration

```hcl
provider "xiac" {
  api_key  = var.xiac_api_key       # optional; falls back to XIAC_API_KEY
  endpoint = "http://127.0.0.1:8099" # optional; falls back to XIAC_ENDPOINT, default https://api.xiac.co (hosted platform)
}
```

| Attribute  | Type   | Required | Description                                                                                     |
|------------|--------|----------|-------------------------------------------------------------------------------------------------|
| `api_key`  | string | no\*     | Tenant API key sent as the `X-Api-Key` header. Falls back to `XIAC_API_KEY`. Sensitive.         |
| `endpoint` | string | no       | Platform base URL. Falls back to `XIAC_ENDPOINT`, then defaults to the hosted `https://api.xiac.co`.      |

\* `api_key` must be resolvable from either the config block or `XIAC_API_KEY`;
the provider errors during configure if it is empty.

## Resource: `xiac_aws_account`

Connects one AWS account to XIaC via a cross-account **read-only** role.

```hcl
data "aws_region" "current" {}

resource "xiac_aws_account" "demo" {
  iam_role    = aws_iam_role.xiac_role.arn
  external_id = random_uuid.xiac_external_id.result
  sts_region  = data.aws_region.current.region
  regions     = ["us-east-1"]
  readonly    = true
}
```

### Arguments

| Attribute | Type | Required | Default | Description |
|------------|------|----------|---------|-------------|
| `provider_id` | string | no | — | Existing tenant-scoped provider ID from XIaC app onboarding. Omit for standalone creation. |
| `iam_role` | string | yes | — | The cross-account IAM role ARN XIaC assumes to scan the account. |
| `external_id` | string | yes | — | Client-generated UUID used in both the AWS trust policy and XIaC registration. |
| `sts_region` | string | yes | — | AWS region used for the STS AssumeRole bootstrap call. |
| `regions` | list(string) | no | — | AWS regions to scan. Empty means the platform's default region set. |
| `readonly` | bool | no | `true` | Only `true` is supported today; setting `false` returns an error. |

### Attributes (computed)

| Attribute     | Description                                                        |
|---------------|--------------------------------------------------------------------|
| `id`          | Platform-assigned provider id.                                     |
| `account_id`  | The AWS account id discovered when the role is verified.           |
| `status`      | Connection status (e.g. `connected`, `error`).                     |

## External ID ownership

The client generates the External ID before either side is created. Use a
`random_uuid` resource, put its `result` in the IAM role's `sts:ExternalId`
condition, and pass the same value to `xiac_aws_account.external_id`. XIaC
validates and stores that UUID verbatim; it does not generate a replacement.

This removes the creation-order cycle: Terraform can create the UUID and role,
then register and verify the account with XIaC in one dependency graph. See
[`examples/main.tf`](examples/main.tf) for the complete wiring.

## App-driven adoption

When XIaC web onboarding has already created the pending provider, pass its
`provider_id` and the exact same `external_id`. The provider reads that
tenant-scoped record, rejects any External ID mismatch, updates its STS/scan
regions, and connects the role. Adoption never falls back to creating a second
provider after an error.

## Development

```sh
go build ./...
go test ./...
```

Acceptance tests (if added) are gated behind `TF_ACC` per the framework
convention and are not run by `go test ./...`.
