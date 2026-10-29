// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.content.Context
import android.content.Intent
import io.github.thienvu18.kylok.proto.v1.InstalledApplication
import io.github.thienvu18.kylok.proto.v1.SyncResponse
import org.json.JSONObject

internal enum class RecoveryOutcome {
    NOT_OWNER,
    CLEARED,
    PREREQUISITES_PENDING,
    DEVICE_LOCKED,
    ALLOWLIST_PENDING,
    NOT_LAUNCHABLE,
    ALREADY_RUNNING,
    THROTTLED,
    LAUNCHED
}

internal data class RecoveryResult(
    val outcome: RecoveryOutcome,
    val kioskPackage: String = "",
    val throttleDelayMs: Long = 0L,
    val crashCount: Int = 0
)

internal interface LauncherPlatform {
    /** Read wall clock time in milliseconds for recovery attempt throttling. */
    fun now(): Long
    /** Confirm Device Owner privilege before launcher mutations. */
    fun isOwner(): Boolean
    /** Query whether the keyguard is currently locked. */
    fun isDeviceLocked(): Boolean
    /** Verify whether the target package is permitted in Lock Task mode. */
    fun isLockTaskPermitted(packageName: String): Boolean
    /** Read effective Lock Task mode state from the platform. */
    fun isLockTaskLocked(): Boolean
    /** Check whether Kylok's HOME activity is the persistent preferred activity. */
    fun isHomePreferred(): Boolean
    /** Register or clear Kylok's HOME activity as the persistent preferred activity. */
    fun setHomePreferred(preferred: Boolean)
    /** Look up an installed application and its signing certificate digest. */
    fun installedApp(packageName: String): InstalledApplication?
    /** Determine whether the target package has a valid launchable activity. */
    fun isAppLaunchable(packageName: String): Boolean
    /** Start the target application with Lock Task mode enabled. */
    fun launchAppInLockTask(packageName: String)
    /** Request the launcher activity to finish and return to the default system launcher. */
    fun finishLauncher()
    /** Persist crash count and recovery backoff state across activity and process lifecycles. */
    fun updateRecovery(kioskPackage: String, crashCount: Int, throttledUntil: Long, lastLaunchAt: Long)
}

/** State reconciliation, crash rate-limiting, and recovery for persistent HOME and kiosk applications. */
internal object LauncherRecovery {
    // A crash within 2 seconds of launch indicates rapid failure rather than user exit.
    const val RAPID_CRASH_THRESHOLD_MS = 2000L
    // Initial delay for crash throttling backoff.
    const val MIN_BACKOFF_DELAY_MS = 1000L
    // Maximum delay capping exponential recovery backoff to avoid prolonged downtime.
    const val MAX_BACKOFF_DELAY_MS = 10000L
    // Stable execution duration (15 seconds) after which rapid crash counters reset.
    const val RECOVERY_RESET_INTERVAL_MS = 15000L

    /** Reconcile persistent launcher and Lock Task state against authenticated snapshot. */
    fun reconcile(state: JSONObject, platform: LauncherPlatform): RecoveryResult {
        if (!platform.isOwner()) {
            platform.finishLauncher()
            return RecoveryResult(RecoveryOutcome.NOT_OWNER)
        }
        if (!state.has("snapshot")) {
            if (platform.isHomePreferred()) platform.setHomePreferred(false)
            platform.finishLauncher()
            return RecoveryResult(RecoveryOutcome.CLEARED)
        }
        val snapshot = SyncResponse.parseFrom(decode(state.getString("snapshot")))
        if (!snapshot.hasDesiredPolicy() || !snapshot.desiredPolicy.hasPolicy()) {
            if (platform.isHomePreferred()) platform.setHomePreferred(false)
            platform.finishLauncher()
            return RecoveryResult(RecoveryOutcome.CLEARED)
        }
        val policy = snapshot.desiredPolicy.policy
        if (!policy.hasLockTask() || policy.lockTask.kioskPackage.isEmpty()) {
            if (platform.isHomePreferred()) platform.setHomePreferred(false)
            platform.finishLauncher()
            return RecoveryResult(RecoveryOutcome.CLEARED)
        }
        val kiosk = policy.lockTask.kioskPackage
        val apps = snapshot.desiredApplicationsList.associateBy { it.artifact.packageName }
        for (requiredName in policy.requiredPackagesList) {
            val want = apps[requiredName]?.artifact
            val actual = platform.installedApp(requiredName)
            val matches = want != null && actual != null &&
                actual.packageName == requiredName &&
                actual.versionCode == want.versionCode &&
                actual.signingCertificatesList.sorted() == want.signingCertificatesList.sorted()
            if (!matches) {
                // Prerequisites missing or updating; hold launcher screen safely.
                return RecoveryResult(RecoveryOutcome.PREREQUISITES_PENDING, kiosk)
            }
        }
        // Ensure persistent HOME is configured for the agent.
        if (!platform.isHomePreferred()) {
            platform.setHomePreferred(true)
        }
        if (platform.isDeviceLocked()) {
            return RecoveryResult(RecoveryOutcome.DEVICE_LOCKED, kiosk)
        }
        if (!platform.isLockTaskPermitted(kiosk)) {
            return RecoveryResult(RecoveryOutcome.ALLOWLIST_PENDING, kiosk)
        }
        if (!platform.isAppLaunchable(kiosk)) {
            return RecoveryResult(RecoveryOutcome.NOT_LAUNCHABLE, kiosk)
        }
        // If already locked into lock task mode with current package, avoid redundant launch.
        if (platform.isLockTaskLocked() && state.optString("lock_task_package") == kiosk) {
            return RecoveryResult(RecoveryOutcome.ALREADY_RUNNING, kiosk)
        }
        // Evaluate crash loop rate limiting
        val recoveryJournal = state.objectValue("launcher_recovery")
        val now = platform.now()
        val lastKiosk = recoveryJournal.optString("kiosk_package")
        var crashCount = recoveryJournal.optInt("crash_count", 0)
        var throttledUntil = recoveryJournal.optLong("throttled_until", 0L)
        var lastLaunch = recoveryJournal.optLong("last_launch_at", 0L)

        if (lastKiosk.isNotEmpty() && lastKiosk != kiosk) {
            crashCount = 0
            throttledUntil = 0L
            lastLaunch = 0L
            recoveryJournal.put("kiosk_package", kiosk)
            recoveryJournal.put("crash_count", 0)
            recoveryJournal.put("throttled_until", 0L)
            recoveryJournal.put("last_launch_at", 0L)
            platform.updateRecovery(kiosk, 0, 0L, 0L)
        }

        if (throttledUntil > now) {
            return RecoveryResult(RecoveryOutcome.THROTTLED, kiosk, throttleDelayMs = throttledUntil - now, crashCount = crashCount)
        }
        if (throttledUntil == 0L && lastLaunch > 0L && now - lastLaunch < RAPID_CRASH_THRESHOLD_MS) {
            crashCount++
            recoveryJournal.put("crash_count", crashCount)
            val backoff = minOf(crashCount * MIN_BACKOFF_DELAY_MS, MAX_BACKOFF_DELAY_MS)
            val until = now + backoff
            recoveryJournal.put("throttled_until", until)
            platform.updateRecovery(kiosk, crashCount, until, lastLaunch)
            return RecoveryResult(RecoveryOutcome.THROTTLED, kiosk, throttleDelayMs = backoff, crashCount = crashCount)
        }
        if (now - lastLaunch >= RECOVERY_RESET_INTERVAL_MS) {
            crashCount = 0
            recoveryJournal.put("crash_count", 0)
        }
        recoveryJournal.put("kiosk_package", kiosk)
        recoveryJournal.put("throttled_until", 0L)
        recoveryJournal.put("last_launch_at", now)
        platform.updateRecovery(kiosk, crashCount, 0L, now)
        platform.launchAppInLockTask(kiosk)
        return RecoveryResult(RecoveryOutcome.LAUNCHED, kiosk, crashCount = crashCount)
    }

    /** Trigger launcher recovery from a background lifecycle callback when kiosk policy is active. */
    fun recover(context: Context) {
        if (!Kylok.owner(context)) return
        val state = runCatching { AgentStorage.read(context) }.getOrNull() ?: return
        if (!state.has("snapshot")) return
        val snapshot = runCatching { SyncResponse.parseFrom(decode(state.getString("snapshot"))) }.getOrNull() ?: return
        if (!snapshot.hasDesiredPolicy() || !snapshot.desiredPolicy.hasPolicy()) return
        val policy = snapshot.desiredPolicy.policy
        if (policy.hasLockTask() && policy.lockTask.kioskPackage.isNotEmpty()) {
            val intent = Intent(context, KylokHomeActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            context.startActivity(intent)
        }
    }
}
