# Security policy

## Reporting a vulnerability

Report suspected vulnerabilities privately through GitHub's security advisory
form on this repository, under the Security tab, rather than opening a public
issue. Include the provider version, the Terraform or OpenTofu version, what you
observed, and the smallest configuration that reproduces it.

Expect an acknowledgement within a few working days. This is a small project
and there is no paid bounty.

## What is in scope

- A configuration that makes the provider send Amazon Connect content the
  author did not write, or that reaches a deployed flow while bypassing the
  lint rules that block a literal ARN or an unresolved reference.
- Credentials or account identifiers written into state, plan output, logs or
  diagnostics beyond what Terraform itself records.
- The release artifacts: a binary, checksum or signature that does not match
  what the release workflow built.

Out of scope: vulnerabilities in Terraform, OpenTofu, the AWS SDK or Amazon
Connect themselves; report those upstream.
