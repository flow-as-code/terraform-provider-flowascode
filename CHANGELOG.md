# Changelog

All notable changes to this provider. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/). Each release names the
flow-as-code `conformance/` commit it vendors and the FlowDoc format version
it reads.

## Unreleased

- Provider block on aws-sdk-go-base with hashicorp/aws's authentication
  vocabulary.
- `flowascode_contact_flow` and `flowascode_contact_flow_module`: flows
  authored as `action` blocks, one typed sub-block per modeled action type
  (built from the vendored catalog) or `generic {}` for the rest; references
  as keys bound through `refs`. The plan shows the canonical FlowDoc
  (`flowdoc`), the content Connect will hold (`content`) and its hash.
- Lint at plan time: the twelve flow-as-code rules, hard rules as errors and
  the rest as warnings, with `lint { disable = [...] }` for the soft ones.
- Drift in Connect shows as changed action blocks; `terraform import` reads
  a live flow back as blocks, recovering `refs` bindings from the instance's
  inventory; `moved` from `aws_connect_contact_flow` and
  `aws_connect_contact_flow_module` (hashicorp/aws) without recreating.
- `flowascode_contact_flow_module_version`, keyed to the module's
  `content_hash` and refusing to snapshot a module whose live content
  differs, and `flowascode_contact_flow_module_alias`, repointed in place.
- `flowascode_view` data source.
- Compatibility: FlowDoc 0.2 (`flowdoc-0.2.schema.json`), Terraform 1.8 and
  later, OpenTofu 1.10 and later. The vendored `conformance/` commit is
  recorded in `internal/conformance/COMMIT` and named here at release.
