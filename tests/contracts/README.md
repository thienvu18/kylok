# Development protocol

`proto/kylok/v1/agent.proto` is the current wire contract; both SDKs regenerate from it. Contract tests validate the current Go and Android implementations, generated descriptors and host behavior. Build both SDKs from the same source revision and use fresh development data when local formats change.

Run `make generate schema`, `./scripts/check-generation.sh` and Go/JVM HTTP/HTTPS interoperability after contract changes. Rebuild both hosts and use fresh development data when formats change. Restart/retry guarantees apply to the same build and schema.

Release manifests pair SDKs from one source revision with protocol 1. [Stable readiness](../../docs/release-readiness.md) and the [implementation plan](../../docs/implementation-plan.md) define the remaining qualification work. [Testing](../../docs/testing.md) describes current checks and planned additions.
