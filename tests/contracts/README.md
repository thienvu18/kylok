# Development protocol

`proto/kylok/v1/agent.proto` is the current wire contract. Both SDKs regenerate from it. Before a public release, API, schema and local database changes may break earlier development builds; no frozen compatibility baseline or migration guarantee applies.

Run `make generate schema` and the Go/JVM interoperability tests after changes. Rebuild both hosts and use fresh development data when the contract changes. Restart/retry guarantees apply to the same build and schema.

M6 adds versioned distributions and external consumer checks, not a frozen development baseline. Release manifests pair SDK versions from one source revision with protocol 1; tagged stable v1 publication requires the roadmap's acceptance evidence.
