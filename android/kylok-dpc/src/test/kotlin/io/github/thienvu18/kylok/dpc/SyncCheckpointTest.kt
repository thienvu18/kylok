// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import io.github.thienvu18.kylok.proto.v1.*
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class SyncCheckpointTest {
    @Test fun policyObservationsAreFrozenWithRequestAndNewerOnesSurviveResponseLoss() {
        val storage = state()
        val first = PolicyReport.newBuilder().setRevision(1).setSnapshotRevision(1).build()
        storage.update { it.put("revision", 1).put("policy_report", encode(first)) }
        val request = SyncCheckpoint(storage).request(Inventory.getDefaultInstance())
        val newer = first.toBuilder().addResults(PolicyResult.newBuilder().setRuleId("cleanup").setObservedValue("complete")
            .setState(ComplianceState.COMPLIANCE_STATE_APPLIED)).build()
        storage.update { it.put("policy_report", encode(newer)) }
        assertEquals(request, SyncCheckpoint(storage).request(Inventory.getDefaultInstance()))
        SyncCheckpoint(storage).accept(response(request.sequence, 1))
        assertEquals(newer, SyncCheckpoint(storage).request(Inventory.getDefaultInstance()).policyReport)
    }
    @Test fun policyCannotDisappearRegressOrChangeWithoutANewRevision() {
        val storage = state(); val checkpoint = SyncCheckpoint(storage)
        val policy = DesiredPolicy.newBuilder().setRevision(2).setPolicy(Policy.newBuilder().setCameraDisabled(true)).build()
        checkpoint.request(Inventory.getDefaultInstance())
        checkpoint.accept(response(1, 2).toBuilder().setDesiredPolicy(policy).build())
        checkpoint.request(Inventory.getDefaultInstance())
        val before = storage.read().toString()
        for (invalid in listOf(response(2, 3),
            response(2, 3).toBuilder().setDesiredPolicy(policy.toBuilder().setRevision(1)).build(),
            response(2, 3).toBuilder().setDesiredPolicy(policy.toBuilder().setPolicy(Policy.newBuilder().setCameraDisabled(false))).build())) {
            try { checkpoint.accept(invalid); fail("accepted changed/stale policy") } catch (_: IllegalArgumentException) { }
            assertEquals(before, storage.read().toString())
        }
        checkpoint.accept(response(2, 3).toBuilder().setDesiredPolicy(policy.toBuilder().setRevision(3)
            .setPolicy(Policy.newBuilder().setCameraDisabled(false))).build())
    }
    @Test fun cancellationStopsRetainedWorkWithUnchangedDesiredRevision() {
        val storage = state(); val checkpoint = SyncCheckpoint(storage)
        val artifact = Artifact.newBuilder().setPackageName("io.test.app").setSha256("a".repeat(64)).build()
        val operation = Operation.newBuilder().setId("o".repeat(43)).setRevision(1).setArtifact(artifact)
            .setLeaseId("b".repeat(48)).setDeadlineUnixMillis(100_000).build()
        val desired = DesiredApplication.newBuilder().setArtifact(artifact).setRevision(1).build()
        checkpoint.request(Inventory.getDefaultInstance())
        checkpoint.accept(response(1, 1).toBuilder().addDesiredApplications(desired).addOperations(operation).build())
        val request = checkpoint.request(Inventory.getDefaultInstance())
        assertEquals(listOf(operation.id), request.retainedOperationIdsList)
        val restarted = MemoryState(storage.read())
        assertEquals(request, SyncCheckpoint(restarted).request(Inventory.getDefaultInstance()))
        SyncCheckpoint(restarted).accept(response(2, 1).toBuilder().addDesiredApplications(desired).addStoppedOperationIds(operation.id).build())
        assertFalse(restarted.read().objectValue("operations").has(operation.id))
    }
    @Test fun unrelatedStopMarkerCannotAlterLocalState() {
        val storage = state(); val checkpoint = SyncCheckpoint(storage)
        checkpoint.request(Inventory.getDefaultInstance())
        val before = storage.read().toString()
        try { checkpoint.accept(response(1).toBuilder().addStoppedOperationIds("x".repeat(43)).build()); fail("unbound stop accepted") }
        catch (_: IllegalArgumentException) { }
        assertEquals(before, storage.read().toString())
    }
    @Test fun oneShotDeliveryKeepsArmedCheckpointUntilReportReceipt() {
        val storage = state(); val checkpoint = SyncCheckpoint(storage)
        val command = Command.newBuilder().setId("c".repeat(43)).setKind("REBOOT")
            .setDispatchToken("a".repeat(48)).setDeadlineUnixMillis(100_000).build()
        checkpoint.request(Inventory.getDefaultInstance())
        checkpoint.accept(response(1).toBuilder().addCommands(command).build())
        storage.update { it.objectValue("commands").getJSONObject(command.id).put("stage", "armed").put("boot_before", 7) }
        checkpoint.request(Inventory.getDefaultInstance())
        checkpoint.accept(response(2).toBuilder().addCommands(command).build())
        assertEquals("armed", storage.read().objectValue("commands").getJSONObject(command.id).getString("stage"))
        val result = CommandReport.newBuilder().setCommandId(command.id).setDispatchToken(command.dispatchToken).setState("UNCERTAIN").build()
        storage.update { it.objectValue("command_reports").put(command.id, encode(result)) }
        val sent = checkpoint.request(Inventory.getDefaultInstance())
        assertEquals(listOf(result), sent.commandReportsList)
        assertEquals(sent, SyncCheckpoint(MemoryState(storage.read())).request(Inventory.getDefaultInstance()))
        checkpoint.accept(response(3).toBuilder().addCommandReceipts(command.id).build())
        assertFalse(storage.read().objectValue("commands").has(command.id))
        assertFalse(storage.read().objectValue("command_reports").has(command.id))
    }
    @Test fun cancellationOmissionRemovesOnlyUnarmedCommands() {
        val storage = state(); val checkpoint = SyncCheckpoint(storage)
        val command = Command.newBuilder().setId("c".repeat(43)).setKind("LOCK")
            .setDispatchToken("a".repeat(48)).setDeadlineUnixMillis(100_000).build()
        checkpoint.request(Inventory.getDefaultInstance())
        checkpoint.accept(response(1).toBuilder().addCommands(command).build())
        checkpoint.request(Inventory.getDefaultInstance()); checkpoint.accept(response(2))
        assertEquals(0, storage.read().objectValue("commands").length())
    }
    @Test fun networkCredentialsCannotChangeWithinARevisionAndReportsAreFrozenForReplay() {
        val storage = state(); val checkpoint = SyncCheckpoint(storage)
        val profile = WifiProfile.newBuilder().setId("office").setSsid("Office").setSecurity("WPA2_PSK")
            .setPassphrase(com.google.protobuf.ByteString.copyFromUtf8("private-pass")).build()
        val network = DesiredNetworkPolicy.newBuilder().setRevision(1).setPolicy(NetworkPolicy.newBuilder().setPrimaryId("office").addProfiles(profile)).build()
        checkpoint.request(Inventory.getDefaultInstance())
        checkpoint.accept(response(1,1).toBuilder().setDesiredNetworkPolicy(network).build())
        val report = NetworkReport.newBuilder().setRevision(1).setState("PENDING").build()
        storage.update { it.put("network_report",encode(report)) }
        val request = checkpoint.request(Inventory.getDefaultInstance())
        assertEquals(report,request.networkReport)
        storage.update { it.put("network_report",encode(report.toBuilder().setState("FAILED").build())) }
        assertEquals(request,SyncCheckpoint(MemoryState(storage.read())).request(Inventory.getDefaultInstance()))
        val before = storage.read().toString()
        val changed = network.toBuilder().setPolicy(network.policy.toBuilder().setProfiles(0,profile.toBuilder().setPassphrase(com.google.protobuf.ByteString.copyFromUtf8("changed-pass")))).build()
        for (invalid in listOf(response(2,1),response(2,1).toBuilder().setDesiredNetworkPolicy(changed).build())) {
            try { checkpoint.accept(invalid);fail("network revision lost its binding") } catch (_: IllegalArgumentException) { }
            assertEquals(before,storage.read().toString())
        }
        checkpoint.accept(response(2,2).toBuilder().setDesiredNetworkPolicy(changed.toBuilder().setRevision(2)).build())
    }
    private fun state() = MemoryState(JSONObject().put("device_id", "d".repeat(43)).put("scope_id", "site"))
    private fun response(sequence: Long, revision: Long = 0): SyncResponse = SyncResponse.newBuilder()
        .setProtocolVersion(1).setSequence(sequence).setDevice(Device.newBuilder().setId("d".repeat(43))
            .setScopeId("site").setState(DeviceState.DEVICE_STATE_MANAGED).setDesiredRevision(revision)).build()
    @Test fun responseLossKeepsExactRequestAcrossRestart() {
        val storage = state(); val first = SyncCheckpoint(storage).request(Inventory.newBuilder().setModel("before").build())
        val restarted = MemoryState(storage.read())
        val retry = SyncCheckpoint(restarted).request(Inventory.newBuilder().setModel("after").build())
        assertEquals(first, retry)
        SyncCheckpoint(restarted).accept(response(first.sequence))
        assertEquals(2, SyncCheckpoint(restarted).request(Inventory.getDefaultInstance()).sequence)
    }
    @Test fun outOfOrderResponseCannotEraseCheckpoint() {
        val storage = state(); val checkpoint = SyncCheckpoint(storage); checkpoint.request(Inventory.getDefaultInstance())
        val before = storage.read().toString()
        try { checkpoint.accept(response(2)); fail("accepted wrong response") } catch (_: IllegalArgumentException) { }
        assertEquals(before, storage.read().toString())
        checkpoint.accept(response(1, 2))
        checkpoint.request(Inventory.getDefaultInstance())
        try { checkpoint.accept(response(2, 1)); fail("accepted older desired revision") } catch (_: IllegalArgumentException) { }
        assertEquals(2, storage.read().getLong("revision"))
    }
    @Test fun reportsCreatedDuringRequestAreRetainedUntilAcknowledged() {
        val storage = state()
        val first = OperationReport.newBuilder().setId("first").setOperationId("op").build()
        val second = first.toBuilder().setId("second").build()
        storage.update { it.array("reports").put(encode(first)) }
        val checkpoint = SyncCheckpoint(storage)
        val request = checkpoint.request(Inventory.getDefaultInstance())
        storage.update { it.array("reports").put(encode(second)) }
        checkpoint.accept(response(request.sequence).toBuilder().addReceipts(ReportReceipt.newBuilder().setReportId("first").setAccepted(true)).build())
        val next = checkpoint.request(Inventory.getDefaultInstance())
        assertEquals(listOf(second), next.reportsList)
    }
    @Test fun failedPersistenceCannotAdvanceRequestSequence() {
        val storage = state()
        val failing = object : StateAccess {
            override fun read() = storage.read()
            override fun update(change: (JSONObject) -> Unit) { val copy = storage.read(); change(copy); throw java.io.IOException("disk full") }
        }
        try { SyncCheckpoint(failing).request(Inventory.getDefaultInstance()); fail("expected write error") } catch (_: java.io.IOException) { }
        assertFalse(storage.read().has("inflight"))
        assertEquals(1, SyncCheckpoint(storage).request(Inventory.getDefaultInstance()).sequence)
    }
}
