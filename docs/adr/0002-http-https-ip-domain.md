# ADR 0002: HTTP/HTTPS with IP/domain origins

Accepted for development. The configured origin scheme selects HTTP or HTTPS; both support IP addresses and domain names. Optional bootstrap origin is independent. HTTP uses unencrypted HTTP/2 for gRPC; HTTPS verifies certificate/hostname trust with no automatic downgrade.

HTTP does not encrypt tokens/credentials. APK signature/digest checks and device/scope authorization apply to both transports. A bundled private CA changes installed-app trust, not Setup Wizard trust. See [local setup](../local-server.md) and the [protocol](../enrollment-protocol.md).
