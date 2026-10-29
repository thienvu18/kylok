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

    @Test
    fun testQueueReportDeduplicationAndRetention() {
        val storage = state()
        val op = Operation.newBuilder().setId("o".repeat(43)).setLeaseId("l".repeat(48)).build()
        val local = JSONObject()
        storage.update { queueReport(it, local, op, OperationState.OPERATION_STATE_RUNNING) }
        val key = local.getString("reported")
        assertTrue(key.isNotEmpty())
        // Repeated call with same key is a no-op
        val countBefore = storage.read().getJSONArray("reports").length()
        storage.update { queueReport(it, local, op, OperationState.OPERATION_STATE_RUNNING) }
        assertEquals(countBefore, storage.read().getJSONArray("reports").length())

        // Call with new state updates report
        storage.update { queueReport(it, local, op, OperationState.OPERATION_STATE_SUCCEEDED) }
        assertEquals(countBefore, storage.read().getJSONArray("reports").length())
        assertEquals(OperationState.OPERATION_STATE_SUCCEEDED, OperationReport.parseFrom(decode(storage.read().getJSONArray("reports").getString(0))).state)
    }

    @Test
    fun testAcceptResponseSupersedingAndLimits() {
        val storage = state()
        val checkpoint = SyncCheckpoint(storage)
        checkpoint.request(Inventory.getDefaultInstance())

        val artifact1 = Artifact.newBuilder().setPackageName("pkg1").setSha256("1".repeat(64)).build()
        val op1 = Operation.newBuilder().setId("1".repeat(43)).setLeaseId("a".repeat(48))
            .setArtifact(artifact1).setRevision(1).setDeadlineUnixMillis(100000).build()
        val desired1 = DesiredApplication.newBuilder().setArtifact(artifact1).setRevision(1).build()

        // Valid response with operations
        val resp1 = response(1, 1).toBuilder().addDesiredApplications(desired1).addOperations(op1).build()
        checkpoint.accept(resp1)

        // Verify operation saved
        assertTrue(storage.read().getJSONObject("operations").has(op1.id))

        // Next sync request
        checkpoint.request(Inventory.getDefaultInstance())

        // Newer desired application revision supersedes and prunes unreferenced op1
        val desiredNewer = DesiredApplication.newBuilder().setArtifact(artifact1).setRevision(2).build()
        val resp2 = response(2, 2).toBuilder().addDesiredApplications(desiredNewer).build()
        checkpoint.accept(resp2)
        assertFalse(storage.read().getJSONObject("operations").has(op1.id))
    }

    @Test
    fun testRequestLimitsAndValidation() {
        val storage = state()
        val checkpoint = SyncCheckpoint(storage)

        // > 64 retained operation IDs -> throws IllegalArgumentException
        storage.update { state ->
            val ops = state.objectValue("operations")
            for (i in 0..64) {
                ops.put("op-$i", JSONObject().put("terminal", false).put("superseded", false))
            }
        }
        assertThrows(IllegalArgumentException::class.java) { checkpoint.request(Inventory.getDefaultInstance()) }

        // Reset operations
        storage.update { it.remove("operations") }

        // > 16 command reports -> throws IllegalArgumentException
        storage.update { state ->
            val crs = state.objectValue("command_reports")
            for (i in 0..16) {
                val cr = CommandReport.newBuilder().setCommandId("c-$i").setDispatchToken("tok").setState("APPLIED").build()
                crs.put("c-$i", encode(cr))
            }
        }
        assertThrows(IllegalArgumentException::class.java) { checkpoint.request(Inventory.getDefaultInstance()) }

        // Reset command reports
        storage.update { it.remove("command_reports") }

        // > 64 reports in storage -> takes minOf(64, reports.length())
        storage.update { state ->
            val reps = state.array("reports")
            for (i in 0..70) {
                val rep = OperationReport.newBuilder().setId("r-$i").setOperationId("op-x").build()
                reps.put(encode(rep))
            }
        }
        val req = checkpoint.request(Inventory.getDefaultInstance())
        assertEquals(64, req.reportsCount)
    }

    @Test
    fun testAcceptCommandAndOperationValidation() {
        val storage = state()
        val checkpoint = SyncCheckpoint(storage)
        checkpoint.request(Inventory.getDefaultInstance())

        // Device ID mismatch
        val badDev = response(1).toBuilder().setDevice(Device.newBuilder().setId("wrong-id").setScopeId("site").setState(DeviceState.DEVICE_STATE_MANAGED)).build()
        assertThrows(IllegalArgumentException::class.java) { checkpoint.accept(badDev) }

        // Scope ID mismatch
        val badScope = response(1).toBuilder().setDevice(Device.newBuilder().setId("d".repeat(43)).setScopeId("wrong-site").setState(DeviceState.DEVICE_STATE_MANAGED)).build()
        assertThrows(IllegalArgumentException::class.java) { checkpoint.accept(badScope) }

        // CommandsCount > 1
        val c1 = Command.newBuilder().setId("c1".padEnd(43, 'x')).setKind("REBOOT").setDispatchToken("d1".padEnd(48, '0')).setDeadlineUnixMillis(1000).build()
        val c2 = Command.newBuilder().setId("c2".padEnd(43, 'x')).setKind("LOCK").setDispatchToken("d2".padEnd(48, '0')).setDeadlineUnixMillis(1000).build()
        val multiCmd = response(1).toBuilder().addCommands(c1).addCommands(c2).build()
        assertThrows(IllegalArgumentException::class.java) { checkpoint.accept(multiCmd) }

        // Command with invalid kind
        val badKindCmd = c1.toBuilder().setKind("SELF_DESTRUCT").build()
        assertThrows(IllegalArgumentException::class.java) { checkpoint.accept(response(1).toBuilder().addCommands(badKindCmd).build()) }

        // Command with invalid deadline
        val badDeadlineCmd = c1.toBuilder().setDeadlineUnixMillis(0).build()
        assertThrows(IllegalArgumentException::class.java) { checkpoint.accept(response(1).toBuilder().addCommands(badDeadlineCmd).build()) }

        // Operation notBefore >= deadline
        val art = Artifact.newBuilder().setPackageName("com.pkg").setSha256("a".repeat(64)).build()
        val wantApp = DesiredApplication.newBuilder().setArtifact(art).setRevision(1).build()
        val badTimeOp = Operation.newBuilder().setId("o".repeat(43)).setLeaseId("a".repeat(48))
            .setArtifact(art).setRevision(1).setNotBeforeUnixMillis(5000).setDeadlineUnixMillis(4000).build()
        val badOpResp = response(1, 1).toBuilder().addDesiredApplications(wantApp).addOperations(badTimeOp).build()
        assertThrows(IllegalArgumentException::class.java) { checkpoint.accept(badOpResp) }

        // Operation update with changed lease ID clears reported key
        val goodOp = badTimeOp.toBuilder().setNotBeforeUnixMillis(1000).setDeadlineUnixMillis(5000).build()
        val respGood = response(1, 1).toBuilder().addDesiredApplications(wantApp).addOperations(goodOp).build()
        checkpoint.accept(respGood)

        storage.update { state ->
            val opRecord = state.objectValue("operations").getJSONObject(goodOp.id)
            opRecord.put("reported", "old-key")
        }

        // Send same operation with new lease ID
        val goodOpNewLease = goodOp.toBuilder().setLeaseId("b".repeat(48)).build()
        checkpoint.request(Inventory.getDefaultInstance())
        val respNewLease = response(2, 1).toBuilder().addDesiredApplications(wantApp).addOperations(goodOpNewLease).build()
        checkpoint.accept(respNewLease)
        assertFalse(storage.read().objectValue("operations").getJSONObject(goodOp.id).has("reported"))
    }

    @Test
    fun testQueueReportInflightPreservation() {
        val storage = state()
        val art = Artifact.newBuilder().setPackageName("com.pkg").setSha256("a".repeat(64)).build()
        val opA = Operation.newBuilder().setId("opA".padEnd(43, 'a')).setLeaseId("leaseA".padEnd(48, '0')).setArtifact(art).build()
        val opB = Operation.newBuilder().setId("opB".padEnd(43, 'b')).setLeaseId("leaseB".padEnd(48, '0')).setArtifact(art).build()

        val localA = JSONObject()
        val localB = JSONObject()

        // Queue report for opA and opB
        storage.update { queueReport(it, localA, opA, OperationState.OPERATION_STATE_RUNNING) }
        storage.update { queueReport(it, localB, opB, OperationState.OPERATION_STATE_RUNNING) }
        assertEquals(2, storage.read().getJSONArray("reports").length())

        // Inflight sync request containing report for opA
        SyncCheckpoint(storage).request(Inventory.getDefaultInstance())
        assertTrue(storage.read().has("inflight"))

        // Update report for opA while previous report is inflight -> inflight report is preserved!
        storage.update { queueReport(it, localA, opA, OperationState.OPERATION_STATE_SUCCEEDED) }
        val reports = storage.read().getJSONArray("reports")
        assertEquals(3, reports.length()) // opA inflight + opB + opA succeeded
    }
}
