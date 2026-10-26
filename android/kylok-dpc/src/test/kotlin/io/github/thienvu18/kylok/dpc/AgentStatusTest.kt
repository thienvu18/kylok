// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import io.github.thienvu18.kylok.proto.v1.*
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class AgentStatusTest {
    @Test fun newerDesiredRevisionNeverPresentsAnOlderPolicyResultAsApplied() {
        val report = PolicyReport.newBuilder().setRevision(3).setSnapshotRevision(3).addResults(
            PolicyResult.newBuilder().setRuleId("camera").setState(ComplianceState.COMPLIANCE_STATE_APPLIED).setObservedValue("true")).build()
        val value = statusValue(JSONObject().put("revision", 4).put("policy_report", encode(report)), true, true)
        assertEquals(PolicyStatus("camera", "PENDING", "AWAITING_RECONCILIATION", "true"), value.policies.single())
    }
    @Test fun snapshotKeepsDesiredAndObservedAppsSeparateAndExcludesInstallerConfirmationData() {
        val artifact = Artifact.newBuilder().setPackageName("app.example").setVersionCode(2).setSizeBytes(200).build()
        val snapshot = SyncResponse.newBuilder().setDevice(Device.newBuilder().setLastSeenAtUnixMillis(123)
            .setInventory(Inventory.newBuilder().addApplications(InstalledApplication.newBuilder().setPackageName("app.example").setVersionCode(1))))
            .addDesiredApplications(DesiredApplication.newBuilder().setArtifact(artifact)).build()
        val op = Operation.newBuilder().setId("install").setArtifact(artifact).build()
        val state = JSONObject().put("snapshot", encode(snapshot)).put("server", "http://192.168.101.5:8080")
            .put("operations", JSONObject().put("install", JSONObject().put("operation", encode(op)).put("bytes", 100)
                .put("callback", "waiting").put("confirmation", "private-intent")))
        var status = statusValue(state, true, true)
        assertEquals(2, status.applications.single().desiredVersion)
        assertEquals(1L, status.applications.single().observedVersion)
        assertEquals("PENDING", status.applications.single().state)
        assertEquals(100, status.operations.single().downloadedBytes)
        assertEquals("WAITING_USER", status.operations.single().stage)
        assertTrue(status.operations.single().requiresUserAction)
        assertFalse(status.toString().contains("private-intent"))
        assertEquals(123, status.lastSyncUnixMillis)
        state.getJSONObject("operations").getJSONObject("install").put("superseded", true)
        status = statusValue(state, true, true)
        assertFalse(status.operations.single().requiresUserAction)
        assertEquals("SUPERSEDED", status.operations.single().stage)
    }
    @Test fun typedSnapshotIncludesPolicyOutcomeWithoutCredentialsOrRawState() {
        val report = PolicyReport.newBuilder().setRevision(3).setSnapshotRevision(3).addResults(
            PolicyResult.newBuilder().setRuleId("camera").setState(ComplianceState.COMPLIANCE_STATE_PENDING).setReason("OBSERVATION_PENDING")).build()
        val state = JSONObject().put("credential", "private-credential").put("token", "private-token").put("attempt", "private-attempt")
            .put("device_id", "device").put("scope_id", "scope").put("lifecycle", "MANAGED").put("revision", 3).put("policy_report", encode(report))
        val value = statusValue(state, true, true)
        assertEquals("MANAGED", value.lifecycle)
        assertEquals(3, value.desiredRevision)
        assertEquals(PolicyStatus("camera", "PENDING", "OBSERVATION_PENDING"), value.policies.single())
        assertFalse(value.toString().contains("private-"))
        assertEquals(BuildConfig.SDK_VERSION, value.sdkVersion)
    }
    @Test fun failingAndClosedListenersDoNotAffectOtherRegistrations() {
        val listeners = StatusListeners()
        val value = statusValue(JSONObject(), false, false)
        var calls = 0
        listeners.add(StatusListener { throw IllegalStateException("host UI") })
        val subscription = listeners.add(StatusListener { calls++ })
        listeners.deliver(value)
        assertEquals(1, calls)
        subscription.close(); subscription.close()
        listeners.deliver(value)
        assertEquals(1, calls)
    }
}
