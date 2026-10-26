# ADR 0001: Key-bound enrollment recovery

Accepted for development. Persist the device's attempt ID and Keystore P-256 key before enrollment. Atomically store token consumption, identity, credential verifier and a receipt encrypted to that key. Matching attempt/key retries recover the same encrypted response for 24 hours; another binding or revoked device fails.

This handles response loss without returning plaintext credentials to a copied request. Losing the private key requires fresh authorized provisioning. Exact encoding and limits are in the [protocol](../enrollment-protocol.md).
