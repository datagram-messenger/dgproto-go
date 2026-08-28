# Security policy

## Reporting a vulnerability

Do not open a public issue. Report suspected vulnerabilities through the private [GitHub security advisory form](https://github.com/datagram-messenger/dgproto-go/security/advisories/new), including affected versions, impact, and reproduction details.

Security fixes target supported tagged releases and the current default branch. Identify both the module release and protocol draft when reporting wire-level behavior.

## Authentication and authorization

A successful Noise XX handshake authenticates possession of a static key; it does not grant application permissions. `ServerConfig.AllowedClients` can restrict authenticated client keys. Use `ServerConfig.Admission` for application authorization after authentication and before dispatch; see [Admission and authorization](docs/guides/getting-started.md#admission-and-authorization).

Applications remain responsible for key provisioning and rotation, authorization policy, secret storage, dependency updates, endpoint hardening, and padding policy. DGProto v1 permits ChaCha20-Poly1305 only; AES-GCM is not protocol compliant.
