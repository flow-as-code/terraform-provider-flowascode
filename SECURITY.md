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

## Release signing key

Every release's `SHA256SUMS` file carries a detached signature,
`SHA256SUMS.sig`, made with this key and no other:

```
The flow-as-code Authors <info@flow-as-code.dev>
RSA 4096, created 2026-09-29, no expiry
A135 4AB0 D81C 8D73 3730  21E9 2C8E 2160 F589 50AC
```

The public key is `signing-key.asc` in this repository, and the same key is
registered with the Terraform Registry and the OpenTofu registry, which check
the signature at `init`. To check a release by hand:

```sh
gpg --import signing-key.asc
gpg --verify terraform-provider-flowascode_<version>_SHA256SUMS.sig \
  terraform-provider-flowascode_<version>_SHA256SUMS
sha256sum --check --ignore-missing terraform-provider-flowascode_<version>_SHA256SUMS
```

A signature from any other key, or a key with this name and a different
fingerprint, did not come from this project's release workflow; report it as
above. A replacement key would be announced in `CHANGELOG.md` and here.

Out of scope: vulnerabilities in Terraform, OpenTofu, the AWS SDK or Amazon
Connect themselves; report those upstream.
