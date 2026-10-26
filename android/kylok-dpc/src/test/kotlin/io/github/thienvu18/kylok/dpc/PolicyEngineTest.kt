// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import io.github.thienvu18.kylok.proto.v1.*
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.IOException

class PolicyEngineTest {
    private val artifact = Artifact.newBuilder().setPackageName("io.test.app").setVersionCode(2).addSigningCertificates("b".repeat(64)).build()
    private fun app(version: Long = 2, cert: String = "b".repeat(64)) = InstalledApplication.newBuilder().setPackageName(artifact.packageName)
        .setVersionCode(version).addSigningCertificates(cert).build()
    private fun snapshot(policy: Policy, revision: Long = 2) = SyncResponse.newBuilder()
        .setDevice(Device.newBuilder().setDesiredRevision(revision))
        .addDesiredApplications(DesiredApplication.newBuilder().setArtifact(artifact).setRevision(1))
        .setDesiredPolicy(DesiredPolicy.newBuilder().setRevision(revision).setPolicy(policy)).build()
    private fun state(policy: Policy) = MemoryState(JSONObject().put("snapshot", encode(snapshot(policy))))
    private fun policy(kiosk: Boolean = false): Policy = Policy.newBuilder().addRequiredPackages(artifact.packageName)
        .setCameraDisabled(true).setScreenCaptureDisabled(true)
        .addRestrictions(RestrictionRule.newBuilder().setName("no_modify_accounts").setEnabled(true))
        .addPermissions(PermissionRule.newBuilder().setPackageName(artifact.packageName).setPermission("android.permission.CAMERA")
            .setState(PermissionState.PERMISSION_STATE_GRANTED))
        .apply { if (kiosk) lockTask = LockTaskPolicy.newBuilder().addPackages(artifact.packageName).setKioskPackage(artifact.packageName).build() }.build()
    private fun reports(storage: StateAccess) = PolicyReport.parseFrom(decode(storage.read().getString("policy_report"))).resultsList.associateBy { it.ruleId }
    private class Death : Error()
    private class Platform : PolicyPlatform {
        var time = 1L
        var owner = true
        var observed: InstalledApplication? = null
        var crashAfterApply = false
        var asyncKiosk = false
        var noOp = false
        val unsupported = mutableSetOf<String>()
        val denied = mutableSetOf<String>()
        val values = mutableMapOf<String, String>()
        val effects = mutableListOf<PolicySetting>()
        override fun now() = time
        override fun isOwner() = owner
        override fun installed(packageName: String) = observed?.takeIf { it.packageName == packageName }
        override fun check(setting: PolicySetting) { if (setting.id in unsupported) throw PolicyUnsupported("FIXTURE_UNSUPPORTED") }
        override fun capture(setting: PolicySetting) = observe(setting)
        override fun observe(setting: PolicySetting) = values[setting.id] ?: when (setting.kind) {
            PolicyKind.LOCK_TASK -> lockTaskValue(emptyList(), "")
            PolicyKind.PERMISSION -> "DEFAULT"
            PolicyKind.MAXIMUM_TIME_TO_LOCK -> "0"
            else -> "false"
        }
        override fun apply(setting: PolicySetting) {
            effects += setting
            if (setting.id in denied) throw SecurityException("denied")
            if (!noOp && !(asyncKiosk && setting.kind == PolicyKind.LOCK_TASK)) values[setting.id] = setting.value
            if (crashAfterApply) throw Death()
        }
    }
    @Test fun absentPolicyDoesNotTouchSettings() {
        val platform = Platform()
        assertFalse(PolicyEngine(MemoryState(), platform).run())
        assertTrue(platform.effects.isEmpty())
    }
    @Test fun cancellationCannotApplyAnotherSetting() {
        val storage = state(policy())
        val platform = Platform().apply { observed = app() }
        Thread.currentThread().interrupt()
        try {
            try { PolicyEngine(storage, platform).run(); fail("cancelled work ran") } catch (_: IOException) { }
            assertTrue(platform.effects.isEmpty())
            assertFalse(storage.read().has("policy_journal"))
        } finally { Thread.interrupted() }
    }
    @Test fun missingWrongVersionOrSignerPreventsKioskAndPermissionEffects() {
        for (observed in listOf(null, app(1), app(cert = "c".repeat(64)))) {
            val storage = state(policy(true)); val platform = Platform().apply { this.observed = observed }
            assertTrue(PolicyEngine(storage, platform).run())
            assertEquals("REQUIRED_APPS_PENDING", reports(storage).getValue("lock_task").reason)
            assertTrue(platform.effects.none { it.kind == PolicyKind.LOCK_TASK || it.kind == PolicyKind.PERMISSION })
            assertEquals(ComplianceState.COMPLIANCE_STATE_APPLIED, reports(storage).getValue("camera").state)
        }
    }
    @Test fun installedAppAllowsKioskAfterIndependentSettingsAndDuplicateRunHasNoEffects() {
        val storage = state(policy(true)); val platform = Platform().apply { observed = app() }
        assertFalse(PolicyEngine(storage, platform).run())
        assertEquals(PolicyKind.LOCK_TASK, platform.effects.last().kind)
        assertTrue(reports(storage).values.all { it.state == ComplianceState.COMPLIANCE_STATE_APPLIED })
        val count = platform.effects.size
        assertFalse(PolicyEngine(storage, platform).run())
        assertEquals(count, platform.effects.size)
    }
    @Test fun asyncKioskNeedsObservationAndLaunchIsThrottledAcrossRestart() {
        val storage = state(policy(true)); val platform = Platform().apply { observed = app(); asyncKiosk = true }
        assertTrue(PolicyEngine(storage, platform).run())
        assertEquals("OBSERVATION_PENDING", reports(storage).getValue("lock_task").reason)
        val restarted = MemoryState(storage.read())
        assertTrue(PolicyEngine(restarted, platform).run())
        assertEquals(1, platform.effects.count { it.kind == PolicyKind.LOCK_TASK })
        platform.values["lock_task"] = lockTaskValue(listOf(artifact.packageName), artifact.packageName)
        assertFalse(PolicyEngine(restarted, platform).run())
        assertEquals(ComplianceState.COMPLIANCE_STATE_APPLIED, reports(restarted).getValue("lock_task").state)
    }
    @Test fun setterWithoutReadbackRemainsPendingThenRetriesAfterBackoff() {
        val storage = state(Policy.newBuilder().setCameraDisabled(true).build())
        val platform = Platform().apply { noOp = true }
        assertTrue(PolicyEngine(storage, platform).run())
        assertEquals("false", reports(storage).getValue("camera").observedValue)
        platform.noOp = false; platform.time += 30_001
        assertFalse(PolicyEngine(storage, platform).run())
        assertEquals(2, platform.effects.size)
    }
    @Test fun unsupportedAndDeniedRulesDoNotHideIndependentCompliance() {
        val storage = state(policy()); val platform = Platform().apply { observed = app(); unsupported += "screen_capture"; denied += "camera" }
        PolicyEngine(storage, platform).run()
        assertEquals(ComplianceState.COMPLIANCE_STATE_UNSUPPORTED, reports(storage).getValue("screen_capture").state)
        assertEquals(ComplianceState.COMPLIANCE_STATE_FAILED, reports(storage).getValue("camera").state)
        assertEquals(ComplianceState.COMPLIANCE_STATE_APPLIED, reports(storage).getValue("restriction:no_modify_accounts").state)
    }
    @Test fun ownerLossProducesNoEffectsAndKeepsJournalForRecovery() {
        val storage = state(policy(true)); val platform = Platform().apply { observed = app() }
        PolicyEngine(storage, platform).run()
        val before = storage.read().getJSONObject("policy_journal").toString()
        platform.owner = false; platform.effects.clear()
        assertFalse(PolicyEngine(storage, platform).run())
        assertTrue(platform.effects.isEmpty())
        assertEquals(before, storage.read().getJSONObject("policy_journal").toString())
        assertTrue(reports(storage).values.all { it.reason == "DEVICE_OWNER_REQUIRED" })
    }
    @Test fun checkpointFailurePreventsExternalEffect() {
        val original = state(Policy.newBuilder().setCameraDisabled(true).build())
        val failing = object : StateAccess {
            override fun read() = original.read()
            override fun update(change: (JSONObject) -> Unit) { throw IOException("disk full") }
        }
        val platform = Platform()
        try { PolicyEngine(failing, platform).run(); fail("missing fault") } catch (_: IOException) { }
        assertTrue(platform.effects.isEmpty())
    }
    @Test fun crashAfterEffectRetainsOriginalBaselineAndDoesNotApplyTwice() {
        val storage = state(Policy.newBuilder().setCameraDisabled(true).build())
        val platform = Platform().apply { crashAfterApply = true }
        try { PolicyEngine(storage, platform).run(); fail("missing fault") } catch (_: Death) { }
        val restarted = MemoryState(storage.read()); platform.crashAfterApply = false
        assertFalse(PolicyEngine(restarted, platform).run())
        assertEquals(1, platform.effects.size)
        restarted.update { it.put("snapshot", encode(snapshot(Policy.getDefaultInstance(), 3))) }
        assertFalse(PolicyEngine(restarted, platform).run())
        assertEquals("false", platform.values["camera"])
        assertEquals(0, restarted.read().getJSONObject("policy_journal").length())
    }
    @Test fun replacementRestoresBaselinesAndLeavesUnmanagedHostSettingsAlone() {
        val storage = state(policy(true)); val platform = Platform().apply {
            observed = app(); values["screen_capture"] = "true"; values["host_only"] = "untouched"
        }
        PolicyEngine(storage, platform).run()
        storage.update { it.put("snapshot", encode(snapshot(Policy.getDefaultInstance(), 3))) }
        platform.effects.clear()
        assertFalse(PolicyEngine(storage, platform).run())
        assertEquals(PolicyKind.LOCK_TASK, platform.effects.first().kind)
        assertEquals("true", platform.values["screen_capture"])
        assertEquals("DEFAULT", platform.values["permission:${artifact.packageName}:android.permission.CAMERA"])
        assertEquals("untouched", platform.values["host_only"])
        assertEquals(setOf("cleanup"), reports(storage).keys)
        assertEquals("complete", reports(storage).getValue("cleanup").observedValue)
    }
    @Test fun failedRemovalSurvivesRestartAndReportsCleanupFailure() {
        val storage = state(policy(true)); val platform = Platform().apply { observed = app() }
        PolicyEngine(storage, platform).run()
        storage.update { it.put("snapshot", encode(snapshot(Policy.getDefaultInstance(), 3))) }
        platform.denied += "camera"
        PolicyEngine(storage, platform).run()
        assertEquals(ComplianceState.COMPLIANCE_STATE_FAILED, reports(storage).getValue("cleanup").state)
        assertTrue(storage.read().getJSONObject("policy_journal").has("camera"))
        val restarted = MemoryState(storage.read()); platform.denied.clear(); platform.time += 30_001
        assertFalse(PolicyEngine(restarted, platform).run())
        assertEquals("complete", reports(restarted).getValue("cleanup").observedValue)
    }
    @Test fun observedAppDriftExitsOwnedKioskBeforeWaitingForRepair() {
        val storage = state(policy(true)); val platform = Platform().apply { observed = app() }
        PolicyEngine(storage, platform).run()
        platform.observed = null
        assertTrue(PolicyEngine(storage, platform).run())
        assertEquals(lockTaskValue(emptyList(), ""), platform.values["lock_task"])
        assertEquals("REQUIRED_APPS_PENDING", reports(storage).getValue("lock_task").reason)
    }
    @Test fun maximumTimeToLockAndCanonicalValuesAreObserved() {
        val storage = state(Policy.newBuilder().setMaximumTimeToLockMillis(60_000).build())
        val platform = Platform()
        assertFalse(PolicyEngine(storage, platform).run())
        assertEquals("60000", reports(storage).getValue("maximum_time_to_lock").observedValue)
        assertEquals("{\"packages\":[\"io.a\",\"io.b\"],\"kiosk_package\":\"io.b\"}", lockTaskValue(listOf("io.b", "io.a"), "io.b"))
    }
    @Test fun anotherAdminsEffectiveRestrictionDoesNotPreventReleasingOurContribution() {
        val storage = state(Policy.newBuilder().setCameraDisabled(true).build())
        var own = "false"
        val platform = object : PolicyPlatform {
            override fun now() = 1L
            override fun isOwner() = true
            override fun installed(packageName: String): InstalledApplication? = null
            override fun check(setting: PolicySetting) { }
            override fun capture(setting: PolicySetting) = own
            override fun observe(setting: PolicySetting) = "true" // Another admin also disables the camera.
            override fun apply(setting: PolicySetting) { own = setting.value }
        }
        assertFalse(PolicyEngine(storage, platform).run())
        storage.update { it.put("snapshot", encode(snapshot(Policy.getDefaultInstance(), 3))) }
        assertFalse(PolicyEngine(storage, platform).run())
        assertEquals("false", own)
        assertEquals("complete", reports(storage).getValue("cleanup").observedValue)
    }
    @Test fun explicitFalseAndZeroAreDifferentFromOmissionAndRestoreOriginalValues() {
        val storage = state(Policy.newBuilder().setCameraDisabled(false).setMaximumTimeToLockMillis(0).build())
        val platform = Platform().apply { values["camera"] = "true"; values["maximum_time_to_lock"] = "60000" }
        assertFalse(PolicyEngine(storage, platform).run())
        assertEquals("false", platform.values["camera"])
        assertEquals("0", platform.values["maximum_time_to_lock"])
        storage.update { it.put("snapshot", encode(snapshot(Policy.getDefaultInstance(), 3))) }
        assertFalse(PolicyEngine(storage, platform).run())
        assertEquals("true", platform.values["camera"])
        assertEquals("60000", platform.values["maximum_time_to_lock"])
    }
}
