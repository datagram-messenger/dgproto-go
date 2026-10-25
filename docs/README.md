# Documentation

| Document | Status | Purpose |
| --- | --- | --- |
| [DGProto v1](protocol/dgproto-v1.md) | **Normative** | Wire format and protocol requirements |
| [Architecture](architecture/overview.md) | Current, non-normative | Go implementation and concurrency model |
| [Package layout](architecture/package-layout.md) | Current, non-normative | Implemented boundaries, test ownership and authoritative refactor checkpoint |
| [Package layout history](architecture/package-layout-history.md) | **Archived** | Earlier refactor observations and superseded proposals, not current guidance |
| [Getting started](guides/getting-started.md) | Non-normative | Public API usage and lifecycle |
| [ErrorMessage migration](guides/error-message-migration.md) | Breaking wire change | Raw UTF-8 Error, paired rollout/rollback and Rust public u8 limit |
| [Test vectors](../testdata/vectors/README.md) | Supporting | Vector scope and validation levels |
| [Package overview](../doc.go) | API documentation | Concise package contract |

The protocol draft and Go module releases are versioned independently. If explanatory material conflicts with the specification on wire-visible behavior, the specification controls.
