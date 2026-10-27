# Physical device validation — fill date and device alias

Status: NOT RUN. Procedure: [physical test runbook](../physical-device-tests.md).

## Identity and conditions

| Field | Recorded value |
| --- | --- |
| Run ID / tester | |
| Start/end with timezone | |
| Device alias / exact model | |
| Android version / API | |
| Firmware / build fingerprint | |
| Source commit + dirty status + source-tree digest | |
| Go / Android SDK versions | |
| DPC package / receiver / version / SHA-256 / signer digest | |
| App fixture versions / SHA-256 / signer digests | |
| API and bootstrap origins / transport / trust | |
| LAN/SSID configuration / WAN block method and duration | |
| USB access / initial OEM power/autostart settings | |
| Initial policy/permission/lock baseline | |
| Private evidence directory (local only) | |

Use PASS / FAIL / NOT RUN / BLOCKED. Record actual outcomes, including the tested boundary. A failed offline setup followed by an online success remains a failed offline case. Hardware observations apply only to this exact configuration.

## Results

| ID | Case | Status | Expected / actual / time / evidence |
| --- | --- | --- | --- |
| PROV-01 | Factory-reset QR, WAN blocked throughout | NOT RUN | |
| PROV-02 | Signed bootstrap downloaded; Device Owner; one authenticated identity | NOT RUN | |
| PROV-03 | Enrollment interruption / bound response recovery | NOT RUN | |
| PROV-04 | Expired token | NOT RUN | |
| PROV-05 | Consumed token cannot enroll another identity | NOT RUN | |
| SYNC-01 | Background sync without opening DPC; OEM defaults | NOT RUN | |
| SYNC-02 | Server restart, same identity/state | NOT RUN | |
| SYNC-03 | LAN loss/reconnect; unseen revision not reported applied | NOT RUN | |
| SYNC-04 | Reboot and recovery without opening DPC | NOT RUN | |
| SYNC-05 | Verified process interruption / checkpoint recovery | NOT RUN | |
| SYNC-06 | Force-stop and explicit relaunch (separate semantics) | NOT RUN | |
| APP-01 | Signed ordinary app install; OS/observed state | NOT RUN | |
| APP-02 | Same-signer v1→v2 update | NOT RUN | |
| APP-03 | Duplicate key returns same operation; conflicting payload rejected | NOT RUN | |
| APP-04 | Downgrade rejected; restore desired v2 afterwards | NOT RUN | |
| APP-05 | Interrupted download / resume / final verification | NOT RUN | |
| APP-06 | User confirmation accepted/rejected | NOT RUN | |
| APP-07 | Lost callback / interruption around install commit | NOT RUN | |
| APP-08 | Wrong signer rejected, original app intact | NOT RUN | |
| APP-09 | Corrupt upload rejected at server intake | NOT RUN | |
| APP-10 | Corrupt delivered bytes rejected at device boundary | NOT RUN | |
| APP-11 | SDK incompatibility | NOT RUN | |
| APP-12 | ABI/features incompatibility | NOT RUN | |
| APP-13 | Low storage and recovery | NOT RUN | |
| CFG-01 | App receives/applies title/button; separate digest readback | NOT RUN | |
| CFG-02 | Live configuration update | NOT RUN | |
| CFG-03 | App process restart and device reboot preserve values | NOT RUN | |
| CFG-04 | Policy release restores baseline/default app behavior | NOT RUN | |
| CFG-05 | Interrupted application/restoration, journal recovery | NOT RUN | |
| POL-01 | Camera disable/release | NOT RUN | |
| POL-02 | Screen capture disable/release | NOT RUN | |
| POL-03 | Maximum time to lock/release | NOT RUN | |
| POL-04 | Account modification restriction/release | NOT RUN | |
| POL-05 | Add-user restriction/release | NOT RUN | |
| POL-06 | Uninstall restriction/release | NOT RUN | |
| POL-07 | Debugging restriction/release; tested last | NOT RUN | |
| POL-08 | Declared runtime permission GRANTED/DENIED/DEFAULT | NOT RUN | |
| POL-09 | Unknown/undeclared permission truthful failure | NOT RUN | |
| POL-10 | Received cached policy during LAN loss/reboot | NOT RUN | |
| POL-11 | Interrupted policy release restores managed baseline only | NOT RUN | |
| KIOSK-01 | Managed Lock Task entry, not screen pinning | NOT RUN | |
| KIOSK-02 | Missing/wrong app prerequisite prevents/suspends locking | NOT RUN | |
| KIOSK-03 | App prerequisite repair restores observed compliance | NOT RUN | |
| KIOSK-04 | Kiosk replacement with another verified app | NOT RUN | |
| KIOSK-05 | Reboot/offline behavior, no duplicate launch | NOT RUN | |
| KIOSK-06 | Empty policy exits/restores original allowlist | NOT RUN | |
| DPC-01 | Same-signer host self-update v1→v2 | NOT RUN | |
| DPC-02 | Same owner, identity, connectivity and observed result | NOT RUN | |
| DPC-03 | Reboot after replacement / no duplicate effect | NOT RUN | |
| NET-01 | HTTP API/bootstrap with LAN IP | NOT RUN | |
| NET-02 | HTTP bootstrap + private-CA HTTPS API; separate reset/run | NOT RUN | |
| NET-03 | Domain origin/resolution and matching HTTPS trust | NOT RUN | |
| NET-04 | Untrusted HTTPS refused; no downgrade | NOT RUN | |
| NET-05 | Physical API 37 LAN permission (needs that device) | NOT RUN | |
| HOST-01 | Custom HostOptions on an independently packaged host | NOT RUN | |
| REVOKE-01 | Revoked sync/download/recovery rejected; no new identity | NOT RUN | |

## Failures and support limits

For each FAIL/BLOCKED/NOT RUN, identify the missing fixture, OEM restriction or exact failure stage. Include safe error codes and operation/policy revision references. Do not attach a complete database, raw credential/QR data or signing secrets. A simulated test or a build cannot replace a physical observation.

## Evidence review

- [ ] Source, APK digests/signers and exact firmware are recorded.
- [ ] WAN conditions are supported by network evidence, separately from UI indicators.
- [ ] Desired state, DPC readback and actual app/OS behavior are distinguished.
- [ ] Process interruption/installer stage was observed, rather than inferred from a command exit code.
- [ ] Photos/log excerpts reviewed for tokens, QR codes, credentials, keys and personal identifiers.
- [ ] Compatibility/readiness updates limited to cases actually demonstrated here.
