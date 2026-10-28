// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import io.github.thienvu18.kylok.proto.v1.*
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.IOException

internal class MemoryState(initial: JSONObject = JSONObject()) : StateAccess {
    private var encoded = initial.toString()
    override fun read() = JSONObject(encoded)
    override fun update(change: (JSONObject) -> Unit) {
        val copy = read(); change(copy); encoded = copy.toString()
    }
}

class InstallEngineTest {
    private val artifact = Artifact.newBuilder().setSha256("a".repeat(64)).setPackageName("io.test.app")
        .setVersionCode(2).addSigningCertificates("b".repeat(64)).setMinSdk(33).setSizeBytes(100).build()
    private val operation = Operation.newBuilder().setId("o".repeat(43)).setRevision(1).setArtifact(artifact)
        .setLeaseId("c".repeat(48)).setDeadlineUnixMillis(100_000).build()
    private fun state() = MemoryState(JSONObject().put("device_id", "d".repeat(43)).put("scope_id", "site")
        .put("operations", JSONObject().put(operation.id, JSONObject().put("operation", encode(operation)))))
    private fun reports(state: StateAccess): List<OperationReport> {
        val reports = state.read().array("reports")
        return (0 until reports.length()).map { OperationReport.parseFrom(decode(reports.getString(it))) }
    }
    private inner class Platform : InstallPlatform {
        var time = 1L; var owner = true; var observed: InstalledApplication? = null
        var exists = false; var sealed = false; var downloads = 0; var creates = 0; var commits = 0
        var afterWriteTime: Long? = null
        var verificationFailure: String? = null; var downloadFailure = false; var commitCrash = false
        override fun now() = time
        override fun isOwner() = owner
        override fun installed(packageName: String) = observed
        override fun download(op: Operation, progress: (Long) -> Unit) {
            downloads++; progress(40)
            if (downloadFailure) throw IOException("LAN lost")
            progress(100)
        }
        override fun verify(op: Operation) { verificationFailure?.let { throw InstallRejected(it) } }
        override fun createSession(op: Operation): Int { creates++; exists = true; return 9 }
        override fun writeSession(id: Int, op: Operation) { assertEquals(9, id); afterWriteTime?.let { time = it } }
        override fun sessionExists(id: Int) = exists
        override fun sessionSealed(id: Int) = sealed
        override fun commit(id: Int, operationID: String) { commits++; sealed = true; if (commitCrash) throw SimulatedProcessDeath() }
        override fun abandon(id: Int) { exists = false }
        override fun cleanup(retained: Set<String>, sessions: Set<Int>) { }
    }
    private class SimulatedProcessDeath : Error()
    private fun installed(version: Long = 2, signer: String = "b".repeat(64)) = InstalledApplication.newBuilder()
        .setPackageName(artifact.packageName).setVersionCode(version).addSigningCertificates(signer).build()

    @Test fun windowPreventsEarlyEffectsAcrossRestartAndClockRollback() {
        val state = state(); val platform = Platform()
        val scheduled = operation.toBuilder().setNotBeforeUnixMillis(50_000).build()
        state.update { it.objectValue("operations").getJSONObject(operation.id).put("operation", encode(scheduled)) }
        assertTrue(InstallEngine(state, platform).run())
        assertEquals(0, platform.downloads); assertEquals(0, platform.commits)
        platform.time = 50_000; platform.afterWriteTime = 49_999
        assertTrue(InstallEngine(state, platform).run())
        assertEquals(0, platform.commits)
        platform.time = 50_000; platform.afterWriteTime = null
        assertTrue(InstallEngine(MemoryState(state.read()), platform).run())
        assertEquals(1, platform.commits); assertEquals(1, platform.creates)
    }
    @Test fun deadlineDuringStagingPreventsCommit() {
        val state = state(); val platform = Platform().apply { afterWriteTime = 100_000 }
        assertFalse(InstallEngine(state, platform).run())
        assertEquals(0, platform.commits)
        assertEquals("DEADLINE_EXCEEDED", reports(state).last().errorCode)
        assertFalse(platform.exists)
    }
    @Test fun commitIsPendingUntilObservedPackageConfirms() {
        val state = state(); val platform = Platform()
        assertTrue(InstallEngine(state, platform).run())
        assertEquals(1, platform.commits)
        assertEquals(OperationState.OPERATION_STATE_RUNNING, reports(state).last().state)
        platform.observed = installed()
        assertFalse(InstallEngine(state, platform).run())
        assertEquals(OperationState.OPERATION_STATE_SUCCEEDED, reports(state).last().state)
        assertEquals(installed(), reports(state).last().observed)
        InstallEngine(state, platform).run()
        assertEquals(1, reports(state).size)
        assertEquals(1, platform.creates)
    }
    @Test fun restartAfterCommitDoesNotCreateOrCommitSecondSession() {
        val state = state(); val platform = Platform().apply { commitCrash = true }
        try { InstallEngine(state, platform).run(); fail("fault not injected") } catch (_: SimulatedProcessDeath) { }
        val restarted = MemoryState(state.read())
        platform.commitCrash = false
        assertTrue(InstallEngine(restarted, platform).run())
        assertEquals(1, platform.creates); assertEquals(1, platform.commits)
        platform.exists = false; platform.observed = installed()
        InstallEngine(restarted, platform).run()
        assertEquals(OperationState.OPERATION_STATE_SUCCEEDED, reports(restarted).last().state)
    }
    @Test fun alreadyInstalledAndSelfUpdateRecoveryNeedNoSession() {
        val state = state(); val platform = Platform().apply { observed = installed() }
        assertFalse(InstallEngine(state, platform).run())
        assertEquals(0, platform.creates); assertEquals(0, platform.downloads)
        assertEquals(OperationState.OPERATION_STATE_SUCCEEDED, reports(state).last().state)
    }
    @Test fun pendingUserActionDoesNotBecomeSuccess() {
        val state = state(); val platform = Platform()
        InstallEngine(state, platform).run()
        state.update { it.objectValue("operations").getJSONObject(operation.id).put("callback", "waiting") }
        assertTrue(InstallEngine(state, platform).run())
        assertEquals(OperationState.OPERATION_STATE_WAITING_USER, reports(state).last().state)
        assertEquals("USER_ACTION_REQUIRED", reports(state).last().errorCode)
    }
    @Test fun successCallbackWithoutObservedPackageFailsTruthfully() {
        val state = state(); val platform = Platform()
        InstallEngine(state, platform).run()
        state.update { it.objectValue("operations").getJSONObject(operation.id).put("callback", "success") }
        assertFalse(InstallEngine(state, platform).run())
        assertEquals("INSTALLED_STATE_MISMATCH", reports(state).last().errorCode)
    }
    @Test fun installerFailureAndMissingSessionAreTerminal() {
        for (callback in listOf("failure", "missing")) {
            val state = state(); val platform = Platform()
            InstallEngine(state, platform).run()
            if (callback == "failure") state.update { it.objectValue("operations").getJSONObject(operation.id).put("callback", "failure").put("callback_code", "PACKAGE_INSTALLER_4") }
            else platform.exists = false
            assertFalse(InstallEngine(state, platform).run())
            assertEquals(OperationState.OPERATION_STATE_FAILED, reports(state).last().state)
            assertEquals(1, platform.creates)
        }
    }
    @Test fun interruptedDownloadResumesAfterStateReload() {
        val state = state(); val platform = Platform().apply { downloadFailure = true }
        assertTrue(InstallEngine(state, platform).run())
        assertEquals(40, state.read().objectValue("operations").getJSONObject(operation.id).getLong("bytes"))
        assertEquals(0, platform.creates)
        platform.downloadFailure = false
        val restarted = MemoryState(state.read())
        assertTrue(InstallEngine(restarted, platform).run())
        assertEquals(1, platform.creates)
    }
    @Test fun digestSignerSdkStorageAndDowngradeFailuresNeverCommit() {
        for (code in listOf("APK_DIGEST_MISMATCH", "SIGNING_IDENTITY_MISMATCH", "INCOMPATIBLE_SDK", "INSUFFICIENT_STORAGE")) {
            val state = state(); val platform = Platform().apply { verificationFailure = code }
            assertFalse(InstallEngine(state, platform).run())
            assertEquals(code, reports(state).last().errorCode); assertEquals(0, platform.commits)
        }
        val state = state(); val platform = Platform().apply { observed = installed(3) }
        InstallEngine(state, platform).run()
        assertEquals("DOWNGRADE_REJECTED", reports(state).last().errorCode)
    }
    @Test fun ownershipLossCannotReportSelfUpdateSuccess() {
        val state = state(); val platform = Platform().apply { observed = installed(); owner = false }
        InstallEngine(state, platform).run()
        assertEquals("DEVICE_OWNER_REQUIRED", reports(state).last().errorCode)
    }
    @Test fun newLeaseReissuesTerminalReportAndSupersessionStopsWork() {
        val state = state(); val platform = Platform().apply { verificationFailure = "SIGNING_IDENTITY_MISMATCH" }
        InstallEngine(state, platform).run()
        state.update { root -> root.objectValue("operations").getJSONObject(operation.id).apply {
            put("operation", encode(operation.toBuilder().setLeaseId("d".repeat(48)).build())); remove("reported")
        } }
        InstallEngine(state, platform).run()
        assertEquals("d".repeat(48), reports(state).last().leaseId)
        state.update { it.objectValue("operations").getJSONObject(operation.id).put("superseded", true) }
        val before = platform.downloads
        InstallEngine(state, platform).run(); assertEquals(before, platform.downloads)
    }
}
