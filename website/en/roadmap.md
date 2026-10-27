# Roadmap

Current source includes N1–N4: host integration, PostgreSQL, durable changes, Android host configuration and typed managed app configuration. The sample app path has device evidence on one POCO Pad X1; physical `HostOptions` coverage remains open. N5–N7 are planned. See [current device validation](compatibility.html).

| Milestone | Outcome |
| --- | --- |
| N1 | Scoped public reads, enrollment correlation and host lifecycle/integration contracts. |
| N2 | Optional PostgreSQL with adapter conformance and concurrent-instance recovery. |
| N3 | Transaction-bound replayable changes and reliable host projections. |
| N4 | Early Android host configuration and typed managed app configuration; sample app receipt/application/release passed on one POCO development build. Separate `HostOptions` hardware coverage remains open. |
| N5 | Groups, bulk assignments, deployment windows and durable reboot/lock commands. |
| N6 | Managed Wi-Fi and private certificate lifecycle with connectivity recovery. |
| N7 | Supported device/firmware matrix, compatibility window, scale evidence and stable release qualification. |

Hosts keep their own users, assets, authorization, workflows and UI. Kylok provides neutral scope/device identifiers and Android management semantics. PostgreSQL and reliable change feeds are available in current source; optional callbacks remain best-effort.

The 2026-10-27 device run also exposed an uninstall-prevention failure. Remediation and retesting are required before stable qualification; WAN-blocked enrollment, kiosk and permission enforcement remain open. The [repository roadmap](https://github.com/thienvu18/kylok/blob/main/ROADMAP.md) defines exit conditions; the [implementation plan](https://github.com/thienvu18/kylok/blob/main/docs/implementation-plan.md) defines interfaces, sequencing and acceptance tests. The [device checklist](https://github.com/thienvu18/kylok/blob/main/docs/provisioning.md) records the remaining checks.
