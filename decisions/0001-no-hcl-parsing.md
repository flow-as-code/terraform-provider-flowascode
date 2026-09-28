# 0001: No HCL parsing in Go; the plan-time `flowdoc` attribute is the parity oracle

Status: accepted, 2026-09-28. The full reasoning is flow-as-code's ADR-0006
(provider shape) and ADR-0007 (HCL as a third view):
https://github.com/flow-as-code/flow-as-code/tree/main/docs/adr

## Decision

This provider never parses a `.tf` file. Terraform or OpenTofu parses and
evaluates the configuration and hands the provider values; the provider turns
those values into a FlowDoc at plan time and exposes it as the computed
`flowdoc` attribute.

Parity with the TypeScript side (`@flow-as-code/hcl`, which does parse `.tf`
text) is proved on that attribute: for every case in the vendored
`conformance/hcl/`, planning the golden resource must produce the case's
FlowDoc byte for byte. The two implementations agree on the contract's
fixtures, not on each other's code.

## Consequences

- Rules that only a text reader can apply (address sugar, `@keep` comments,
  regeneration) are the TypeScript side's alone; the provider refuses what the
  sugar would rewrite, with a message naming the `refs` entry to write.
- A refusal the HCL parser makes before the provider sees the value (a lone
  surrogate escape) passes the corresponding `refuse` case on Terraform's own
  error.
