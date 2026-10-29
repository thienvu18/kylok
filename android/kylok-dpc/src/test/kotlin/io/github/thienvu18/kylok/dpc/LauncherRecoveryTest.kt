// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import io.github.thienvu18.kylok.proto.v1.*
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class LauncherRecoveryTest {
    private val artifact = Artifact.newBuilder().setPackageName("io.test.kiosk").setVersionCode(3)
        .addSigningCertificates("a".repeat(64)).build()

    private fun installed(version: Long = 3, cert: String = "a".repeat(64)) =
        InstalledApplication.newBuilder().setPackageName(artifact.packageName)
            .setVersionCode(version).addSigningCertificates(cert).build()

    private fun snapshot(kioskPackage: String = artifact.packageName, required: List<String> = listOf(artifact.packageName)) =
        SyncResponse.newBuilder()
            .setDevice(Device.newBuilder().setDesiredRevision(1))
            .addDesiredApplications(DesiredApplication.newBuilder().setArtifact(artifact).setRevision(1))
            .setDesiredPolicy(DesiredPolicy.newBuilder().setRevision(1).setPolicy(
                Policy.newBuilder()
                    .addAllRequiredPackages(required)
                    .setLockTask(LockTaskPolicy.newBuilder().addAllPackages(listOf(artifact.packageName)).setKioskPackage(kioskPackage))
            )).build()

    private fun state(kioskPackage: String = artifact.packageName, required: List<String> = listOf(artifact.packageName)) =
        JSONObject().put("snapshot", encode(snapshot(kioskPackage, required)))

    private class FakePlatform : LauncherPlatform {
        var currentTime = 1000L
        var owner = true
        var locked = false
        var lockTaskPermitted = true
        var lockTaskLocked = false
        var preferredHome = false
        var launchable = true
        var finished = false
        var observedApp: InstalledApplication? = null
        val launchedApps = mutableListOf<String>()

        override fun now() = currentTime
        override fun isOwner() = owner
        override fun isDeviceLocked() = locked
        override fun isLockTaskPermitted(packageName: String) = lockTaskPermitted
        override fun isLockTaskLocked() = lockTaskLocked
        override fun isHomePreferred() = preferredHome
        override fun setHomePreferred(preferred: Boolean) { preferredHome = preferred }
        override fun installedApp(packageName: String) = observedApp?.takeIf { it.packageName == packageName }
        override fun isAppLaunchable(packageName: String) = launchable
        override fun launchAppInLockTask(packageName: String) { launchedApps += packageName }
        override fun finishLauncher() { finished = true }
        var recordedKiosk = ""
        var recordedCrashCount = 0
        var recordedThrottledUntil = 0L
        var recordedLastLaunchAt = 0L

        override fun updateRecovery(kioskPackage: String, crashCount: Int, throttledUntil: Long, lastLaunchAt: Long) {
            recordedKiosk = kioskPackage
            recordedCrashCount = crashCount
            recordedThrottledUntil = throttledUntil
            recordedLastLaunchAt = lastLaunchAt
        }
    }

    @Test fun notDeviceOwnerFinishesLauncherAndAborts() {
        val platform = FakePlatform().apply { owner = false }
        val result = LauncherRecovery.reconcile(state(), platform)
        assertEquals(RecoveryOutcome.NOT_OWNER, result.outcome)
        assertTrue(platform.finished)
        assertTrue(platform.launchedApps.isEmpty())
    }

    @Test fun noKioskPolicyClearsPersistentHomeAndFinishesLauncher() {
        val platform = FakePlatform().apply { preferredHome = true }
        val emptyState = JSONObject().put("snapshot", encode(SyncResponse.newBuilder().build()))
        val result = LauncherRecovery.reconcile(emptyState, platform)
        assertEquals(RecoveryOutcome.CLEARED, result.outcome)
        assertFalse(platform.preferredHome)
        assertTrue(platform.finished)
        assertTrue(platform.launchedApps.isEmpty())
    }

    @Test fun missingOrDriftedRequiredAppPrerequisitesSuspendsKioskLaunch() {
        for (app in listOf(null, installed(version = 2), installed(cert = "b".repeat(64)))) {
            val platform = FakePlatform().apply { observedApp = app }
            val result = LauncherRecovery.reconcile(state(), platform)
            assertEquals(RecoveryOutcome.PREREQUISITES_PENDING, result.outcome)
            assertEquals(artifact.packageName, result.kioskPackage)
            assertTrue(platform.launchedApps.isEmpty())
        }
    }

    @Test fun deviceLockedSuspendsLaunchUntilUnlocked() {
        val platform = FakePlatform().apply {
            observedApp = installed()
            locked = true
        }
        val result = LauncherRecovery.reconcile(state(), platform)
        assertEquals(RecoveryOutcome.DEVICE_LOCKED, result.outcome)
        assertTrue(platform.launchedApps.isEmpty())
    }

    @Test fun allowlistPendingSuspendsLaunchUntilPermitted() {
        val platform = FakePlatform().apply {
            observedApp = installed()
            lockTaskPermitted = false
        }
        val result = LauncherRecovery.reconcile(state(), platform)
        assertEquals(RecoveryOutcome.ALLOWLIST_PENDING, result.outcome)
        assertTrue(platform.launchedApps.isEmpty())
    }

    @Test fun notLaunchableApplicationSuspendsLaunch() {
        val platform = FakePlatform().apply {
            observedApp = installed()
            launchable = false
        }
        val result = LauncherRecovery.reconcile(state(), platform)
        assertEquals(RecoveryOutcome.NOT_LAUNCHABLE, result.outcome)
        assertTrue(platform.launchedApps.isEmpty())
    }

    @Test fun cleanLaunchEntersLockTaskModeAndSetsPersistentHome() {
        val platform = FakePlatform().apply {
            observedApp = installed()
            preferredHome = false
        }
        val result = LauncherRecovery.reconcile(state(), platform)
        assertEquals(RecoveryOutcome.LAUNCHED, result.outcome)
        assertEquals(artifact.packageName, result.kioskPackage)
        assertTrue(platform.preferredHome)
        assertEquals(listOf(artifact.packageName), platform.launchedApps)
    }

    @Test fun alreadyRunningAndLockedDoesNotTriggerDuplicateLaunch() {
        val currentState = state().put("lock_task_package", artifact.packageName)
        val platform = FakePlatform().apply {
            observedApp = installed()
            preferredHome = true
            lockTaskLocked = true
        }
        val result = LauncherRecovery.reconcile(currentState, platform)
        assertEquals(RecoveryOutcome.ALREADY_RUNNING, result.outcome)
        assertTrue(platform.launchedApps.isEmpty())
    }

    @Test fun foregroundAppCrashTriggersRelaunch() {
        val platform = FakePlatform().apply {
            observedApp = installed()
            preferredHome = true
            lockTaskLocked = false
            currentTime = 20_000L
        }
        val currentState = state().put("lock_task_package", "")
        val result = LauncherRecovery.reconcile(currentState, platform)
        assertEquals(RecoveryOutcome.LAUNCHED, result.outcome)
        assertEquals(listOf(artifact.packageName), platform.launchedApps)
    }

    @Test fun rapidCrashRateLimitingAndExponentialBackoff() {
        val platform = FakePlatform().apply {
            observedApp = installed()
            preferredHome = true
            currentTime = 10_000L
        }
        val currentState = state()
        // Initial clean launch
        val first = LauncherRecovery.reconcile(currentState, platform)
        assertEquals(RecoveryOutcome.LAUNCHED, first.outcome)
        assertEquals(1, platform.launchedApps.size)

        // Simulate crash 500ms after launch
        platform.currentTime = 10_500L
        platform.lockTaskLocked = false
        val second = LauncherRecovery.reconcile(currentState, platform)
        assertEquals(RecoveryOutcome.THROTTLED, second.outcome)
        assertEquals(1, second.crashCount)
        assertTrue(second.throttleDelayMs > 0L)
        assertEquals(1, platform.launchedApps.size) // No immediate relaunch

        // After backoff duration expires, next reconcile launches
        platform.currentTime += second.throttleDelayMs
        val third = LauncherRecovery.reconcile(currentState, platform)
        assertEquals(RecoveryOutcome.LAUNCHED, third.outcome)
        assertEquals(2, platform.launchedApps.size)
    }

    @Test fun stableExecutionResetsCrashBackoffCounter() {
        val platform = FakePlatform().apply {
            observedApp = installed()
            preferredHome = true
            currentTime = 10_000L
        }
        val currentState = state()
        LauncherRecovery.reconcile(currentState, platform)

        // Rapid crash
        platform.currentTime = 10_500L
        LauncherRecovery.reconcile(currentState, platform)
        assertEquals(1, currentState.objectValue("launcher_recovery").optInt("crash_count"))

        // App runs stably for >= 15 seconds then exits
        platform.currentTime = 30_000L
        platform.lockTaskLocked = false
        val result = LauncherRecovery.reconcile(currentState, platform)
        assertEquals(RecoveryOutcome.LAUNCHED, result.outcome)
        assertEquals(0, result.crashCount)
        assertEquals(0, currentState.objectValue("launcher_recovery").optInt("crash_count"))
    }

    @Test fun appUpdateReplacementAllowsLaunchWhenNewVersionReady() {
        val platform = FakePlatform().apply {
            observedApp = installed(version = 4) // updated version
            preferredHome = true
            currentTime = 10_000L
        }
        // Snapshot expecting version 4
        val updatedArtifact = artifact.toBuilder().setVersionCode(4).build()
        val updatedSnapshot = SyncResponse.newBuilder()
            .setDevice(Device.newBuilder().setDesiredRevision(2))
            .addDesiredApplications(DesiredApplication.newBuilder().setArtifact(updatedArtifact).setRevision(2))
            .setDesiredPolicy(DesiredPolicy.newBuilder().setRevision(2).setPolicy(
                Policy.newBuilder()
                    .addRequiredPackages(updatedArtifact.packageName)
                    .setLockTask(LockTaskPolicy.newBuilder().addPackages(updatedArtifact.packageName).setKioskPackage(updatedArtifact.packageName))
            )).build()
        val currentState = JSONObject().put("snapshot", encode(updatedSnapshot))
        val result = LauncherRecovery.reconcile(currentState, platform)
        assertEquals(RecoveryOutcome.LAUNCHED, result.outcome)
        assertEquals(listOf(updatedArtifact.packageName), platform.launchedApps)
    }

    @Test fun policyReleaseClearsPersistentHomeAndFinishes() {
        val platform = FakePlatform().apply {
            observedApp = installed()
            preferredHome = true
        }
        // Desired policy has empty lock_task / kiosk_package
        val releaseSnapshot = SyncResponse.newBuilder()
            .setDevice(Device.newBuilder().setDesiredRevision(3))
            .setDesiredPolicy(DesiredPolicy.newBuilder().setRevision(3).setPolicy(Policy.newBuilder())).build()
        val currentState = JSONObject().put("snapshot", encode(releaseSnapshot))
        val result = LauncherRecovery.reconcile(currentState, platform)
        assertEquals(RecoveryOutcome.CLEARED, result.outcome)
        assertFalse(platform.preferredHome)
        assertTrue(platform.finished)
    }

    @Test fun recoveryStatePersistedViaPlatformCallback() {
        val platform = FakePlatform().apply {
            observedApp = installed()
            currentTime = 10_000L
        }
        val currentState = state()
        LauncherRecovery.reconcile(currentState, platform)
        assertEquals(artifact.packageName, platform.recordedKiosk)
        assertEquals(0, platform.recordedCrashCount)
        assertEquals(0L, platform.recordedThrottledUntil)
        assertEquals(10_000L, platform.recordedLastLaunchAt)

        // Rapid crash persists updated crash count and throttle deadline
        platform.currentTime = 10_500L
        val throttled = LauncherRecovery.reconcile(currentState, platform)
        assertEquals(RecoveryOutcome.THROTTLED, throttled.outcome)
        assertEquals(1, platform.recordedCrashCount)
        assertEquals(10_500L + throttled.throttleDelayMs, platform.recordedThrottledUntil)
    }

    @Test fun kioskPackageChangeResetsCrashCounter() {
        val platform = FakePlatform().apply {
            observedApp = installed()
            currentTime = 10_000L
        }
        val currentState = state()
        LauncherRecovery.reconcile(currentState, platform)

        // Induce crash throttling on package A
        platform.currentTime = 10_500L
        val throttled = LauncherRecovery.reconcile(currentState, platform)
        assertEquals(RecoveryOutcome.THROTTLED, throttled.outcome)
        assertEquals(1, throttled.crashCount)

        // Replace kiosk app with package B in desired policy
        val newArtifact = Artifact.newBuilder().setPackageName("io.test.other").setVersionCode(1).addSigningCertificates("a".repeat(64)).build()
        val newSnapshot = SyncResponse.newBuilder()
            .setDevice(Device.newBuilder().setDesiredRevision(2))
            .addDesiredApplications(DesiredApplication.newBuilder().setArtifact(newArtifact).setRevision(1))
            .setDesiredPolicy(DesiredPolicy.newBuilder().setRevision(2).setPolicy(
                Policy.newBuilder().addRequiredPackages(newArtifact.packageName)
                    .setLockTask(LockTaskPolicy.newBuilder().addPackages(newArtifact.packageName).setKioskPackage(newArtifact.packageName))
            )).build()
        currentState.put("snapshot", encode(newSnapshot))
        platform.observedApp = InstalledApplication.newBuilder().setPackageName(newArtifact.packageName).setVersionCode(1).addSigningCertificates("a".repeat(64)).build()

        // Reconciling with new kiosk package does not inherit old crash throttle
        val launched = LauncherRecovery.reconcile(currentState, platform)
        assertEquals(RecoveryOutcome.LAUNCHED, launched.outcome)
        assertEquals(newArtifact.packageName, launched.kioskPackage)
        assertEquals(0, launched.crashCount)
        assertEquals(newArtifact.packageName, platform.recordedKiosk)
        assertEquals(0, platform.recordedCrashCount)
    }
}
