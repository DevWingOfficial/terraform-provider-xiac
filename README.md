# terraform-provider-xiac

Declare XIaC cloud scopes as Infrastructure-as-Code. The provider uses each
cloud's native identifier and never exposes XIaC database UUIDs.

For AWS, `account_id` is both the Terraform resource identity and XIaC's generic
`scope_id`.

## Installation

```hcl
terraform {
  required_providers {
    xiac = {
      source  = "XiaCOrg/xiac"
      version = "~> 1.0"
    }
  }
}
```

The repository is public, but no release tag is created until this provider and
the `XiaCOrg/xiac-aws-account/aws` module pass together end to end.

## Provider configuration

```hcl
provider "xiac" {}
```

| Attribute | Environment fallback | Required | Description |
| --- | --- | --- | --- |
| `api_key` | `XIAC_API_KEY` | yes, by either source | Tenant API key sent as `X-Api-Key`. |
| `endpoint` | `XIAC_ENDPOINT` | no | Platform URL; defaults to `https://api.xiac.co`. |

Local development:

```sh
export XIAC_API_KEY='tenant-key'
export XIAC_ENDPOINT='http://127.0.0.1:8099'
```

## Resource: `xiac_aws_account`

```hcl
data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

resource "xiac_aws_account" "this" {
  account_id  = data.aws_caller_identity.current.account_id
  iam_role    = aws_iam_role.xiac.arn
  external_id = random_uuid.xiac_external_id.result
  sts_region  = data.aws_region.current.region
  regions     = []
  readonly    = true
}
```

### Arguments

| Attribute | Required | Default | Description |
| --- | --- | --- | --- |
| `account_id` | yes | - | AWS account ID and public XIaC scope identity. |
| `iam_role` | yes | - | Cross-account role ARN XIaC assumes. |
| `external_id` | yes | - | Client-generated UUID used in AWS trust and XIaC registration. |
| `sts_region` | yes | - | Region for the STS AssumeRole bootstrap call. |
| `regions` | no | `[]` | Scan regions; empty lets XIaC resolve enabled regions. |
| `readonly` | no | `true` | Must remain true. Write-capable roles are rejected. |

`status` is computed after XIaC verifies the connection. There is no `id` or
internal provider identifier in the schema or Terraform state.

Changing `account_id` or `external_id` replaces the resource. Read, update, and
delete operations resolve the authenticated tenant's `aws + account_id` scope.

## Recommended AWS module

Most users should use the zero-required-variable module, which creates the
External ID, AWS trust, read-only role, and this resource together:

```hcl
module "xiac_aws_account" {
  source  = "XiaCOrg/xiac-aws-account/aws"
  version = "~> 1.0"
}
```

## Security

- Generate the External ID on the client and use the same UUID in both systems.
- XIaC verifies `sts:GetCallerIdentity` matches the requested `account_id`.
- XIaC rejects a role proven to allow writes.
- Keep the tenant API key in provider configuration or `XIAC_API_KEY`, never in
  module inputs or committed variable files.

## Development

```sh
gofmt -w internal cmd
go vet ./...
go test ./... -count=1
go build ./...
tofu fmt -check -recursive
```
