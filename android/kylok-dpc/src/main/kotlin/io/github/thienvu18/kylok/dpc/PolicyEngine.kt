// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import io.github.thienvu18.kylok.proto.v1.*
import org.json.JSONObject
import java.io.IOException

internal enum class PolicyKind { CAMERA, SCREEN_CAPTURE, MAXIMUM_TIME_TO_LOCK, RESTRICTION, PERMISSION, MANAGED_CONFIGURATION, LOCK_TASK }
internal data class PolicySetting(val id: String, val kind: PolicyKind, val value: String, val target: String = "", val name: String = "") {
    /** Serialize a policy setting for durable baseline restoration. */
    fun json() = JSONObject().put("id", id).put("kind", kind.name).put("value", value).put("target", target).put("name", name)
    companion object {
        /** Decode a previously journaled policy setting for release. */
        fun from(json: JSONObject) = PolicySetting(json.getString("id"), PolicyKind.valueOf(json.getString("kind")),
            json.getString("value"), json.getString("target"), json.getString("name"))
    }
}
/** Canonicalize sorted Lock Task packages and kiosk identity for exact readback comparison. */
internal fun lockTaskValue(packages: List<String>, kiosk: String): String =
    "{\"packages\":[${packages.sorted().joinToString(",") { JSONObject.quote(it) }}],\"kiosk_package\":${JSONObject.quote(kiosk)}}"
/** Encode observed package/version/signers in the policy report format. */
internal fun applicationValue(app: InstalledApplication): String =
    "${app.packageName}@${app.versionCode}:${app.signingCertificatesList.sorted().joinToString(",")}"

internal interface PolicyPlatform {
    /** Read wall time for bounded setter retry suppression. */
    fun now(): Long
    /** Confirm Device Owner before policy enforcement. */
    fun isOwner(): Boolean
    /** Observe an installed required application and its signing identity. */
    fun installed(packageName: String): InstalledApplication?
    /** Throws PolicyUnsupported for a capability/permission the platform cannot implement. */
    fun check(setting: PolicySetting)
    /** Capture only this admin's setting; observation checks the effective result. */
    fun capture(setting: PolicySetting): String
    /** Read effective platform state, including other administrators where applicable. */
    fun observe(setting: PolicySetting): String
    /** Apply one previously journaled setting without claiming observed compliance. */
    fun apply(setting: PolicySetting)
}
internal class PolicyUnsupported(val reason: String) : Exception(reason)
internal class PolicyPending(val reason: String) : Exception(reason)
private class PolicyPersistenceFailure(cause: Exception) : IOException("Policy checkpoint failed", cause)

/** Replayable setters with a durable baseline/attempt journal, followed by readback.
 * No policy is executed until its authenticated snapshot has been committed. */
internal class PolicyEngine(private val storage: StateAccess, private val platform: PolicyPlatform) {
    /** Abort reconciliation before additional effects when its worker is interrupted. */
    private fun active() { if (Thread.currentThread().isInterrupted) throw IOException("Cancelled policy reconciliation") }
    /** Distinguish checkpoint failures from platform failures so effects cannot continue without persistence. */
    private fun persist(change: (JSONObject) -> Unit) {
        try { storage.update(change) } catch (error: Exception) { throw PolicyPersistenceFailure(error) }
    }
    /** Build a bounded structured rule result for authenticated sync. */
    private fun result(id: String, state: ComplianceState, reason: String = "", observed: String = "") =
        PolicyResult.newBuilder().setRuleId(id).setState(state).setReason(reason.take(128)).setObservedValue(observed).build()
    /** Expand a replacement document into ordered setters, placing kiosk after prerequisites. */
    private fun settings(p: Policy): List<PolicySetting> = buildList {
        if (p.hasCameraDisabled()) add(PolicySetting("camera", PolicyKind.CAMERA, p.cameraDisabled.toString()))
        if (p.hasScreenCaptureDisabled()) add(PolicySetting("screen_capture", PolicyKind.SCREEN_CAPTURE, p.screenCaptureDisabled.toString()))
        if (p.hasMaximumTimeToLockMillis()) add(PolicySetting("maximum_time_to_lock", PolicyKind.MAXIMUM_TIME_TO_LOCK, p.maximumTimeToLockMillis.toString()))
        p.restrictionsList.forEach { add(PolicySetting("restriction:${it.name}", PolicyKind.RESTRICTION, it.enabled.toString(), name = it.name)) }
        p.permissionsList.forEach { add(PolicySetting("permission:${it.packageName}:${it.permission}", PolicyKind.PERMISSION,
            it.state.name.removePrefix("PERMISSION_STATE_"), it.packageName, it.permission)) }
        p.managedConfigurationsList.forEach { add(PolicySetting("configuration:${it.packageName}", PolicyKind.MANAGED_CONFIGURATION,
            ConfigurationValues.journal(it.values), it.packageName)) }
        // Kiosk is always last, after prerequisites and independent restrictions.
        if (p.hasLockTask()) add(PolicySetting("lock_task", PolicyKind.LOCK_TASK, lockTaskValue(p.lockTask.packagesList, p.lockTask.kioskPackage)))
    }
    /** Checkpoint each setter attempt, suppress rapid duplicates, then verify platform readback. */
    private fun attempt(setting: PolicySetting, journal: JSONObject, restoring: Boolean = false): String {
        active()
        // Release is about this admin's contribution. Another admin's stricter
        // camera/restriction policy must not leave our completed cleanup stuck.
        // Kiosk exit additionally needs the actual unlocked state.
        /** Compare only this admin's released contribution, but require effective state for enforcement/kiosk. */
        fun readback() = if (restoring && setting.kind != PolicyKind.LOCK_TASK) platform.capture(setting) else platform.observe(setting)
        var observed = readback()
        if (observed == setting.value) return observed
        val now = platform.now()
        val last = journal.optLong("attempt_at", Long.MIN_VALUE)
        // Allow asynchronous setters 30 seconds to settle; a changed target or clock rollback retries immediately.
        if (journal.optString("attempt_value") != setting.value || now < last || now - last >= 30_000 || last == Long.MIN_VALUE) {
            // Even an asynchronous launch is checkpointed before the external effect.
            persist { it.objectValue("policy_journal").getJSONObject(setting.id)
                .put("attempt_value", setting.value).put("attempt_at", now) }
            active()
            platform.apply(setting)
            observed = readback()
        }
        return observed
    }
    /** Translate platform failures into rule outcomes while propagating persistence/transport failures. */
    private fun failure(id: String, error: Exception): PolicyResult = when (error) {
        is IOException -> throw error
        is PolicyUnsupported -> result(id, ComplianceState.COMPLIANCE_STATE_UNSUPPORTED, error.reason)
        is PolicyPending -> result(id, ComplianceState.COMPLIANCE_STATE_PENDING, error.reason)
        is SecurityException -> result(id, ComplianceState.COMPLIANCE_STATE_FAILED, "POLICY_DENIED")
        is UnsupportedOperationException -> result(id, ComplianceState.COMPLIANCE_STATE_UNSUPPORTED, "PLATFORM_UNSUPPORTED")
        else -> result(id, ComplianceState.COMPLIANCE_STATE_FAILED, "PLATFORM_${error.javaClass.simpleName}")
    }

    /** Restore released baselines, reconcile desired settings and persist per-rule observations. */
    fun run(): Boolean {
        active()
        val state = storage.read()
        if (!state.has("snapshot")) return false
        val snapshot = SyncResponse.parseFrom(decode(state.getString("snapshot")))
        if (!snapshot.hasDesiredPolicy()) return false
        val desired = snapshot.desiredPolicy
        val p = desired.policy
        val rules = settings(p)
        val results = mutableListOf<PolicyResult>()
        if (!platform.isOwner()) {
            (p.requiredPackagesList.map { "required:$it" } + rules.map { it.id } + "cleanup").forEach {
                results += result(it, ComplianceState.COMPLIANCE_STATE_FAILED, "DEVICE_OWNER_REQUIRED")
            }
        } else {
            // Release only journaled settings absent from the replacement document.
            // Keep failed restoration entries so a restart can retry them.
            val journal = state.objectValue("policy_journal")
            val wanted = rules.map { it.id }.toSet()
            var cleanup: PolicyResult? = null
            journal.keys().asSequence().toList().sortedBy { if (it == "lock_task") "" else it }.forEach { id ->
                if (id !in wanted) {
                    active()
                    try {
                        val entry = journal.getJSONObject(id)
                        val baseline = PolicySetting.from(entry.getJSONObject("baseline"))
                        if (baseline.kind != PolicyKind.PERMISSION || platform.installed(baseline.target) != null) {
                            platform.check(baseline)
                            val observed = attempt(baseline, entry, restoring = true)
                            if (observed != baseline.value) throw PolicyPending("RESTORATION_PENDING")
                        }
                        persist { it.objectValue("policy_journal").remove(id) }
                    } catch (error: Exception) {
                        val failed = failure("cleanup", error)
                        /** Preserve the most severe cleanup result when several released settings fail. */
                        fun severity(value: PolicyResult) = when (value.state) {
                            ComplianceState.COMPLIANCE_STATE_FAILED -> 3
                            ComplianceState.COMPLIANCE_STATE_UNSUPPORTED -> 2
                            else -> 1
                        }
                        if (cleanup == null || severity(failed) > severity(cleanup!!)) cleanup = failed
                    }
                }
            }
            results += cleanup ?: result("cleanup", ComplianceState.COMPLIANCE_STATE_APPLIED, observed = "complete")
            val apps = snapshot.desiredApplicationsList.associateBy { it.artifact.packageName }
            val ready = mutableMapOf<String, Boolean>()
            for (name in p.requiredPackagesList) {
                val want = apps[name]?.artifact
                val actual = platform.installed(name)
                val matches = want != null && actual != null && actual.packageName == name && actual.versionCode == want.versionCode &&
                    actual.signingCertificatesList.sorted() == want.signingCertificatesList.sorted()
                ready[name] = matches
                results += result("required:$name", if (matches) ComplianceState.COMPLIANCE_STATE_APPLIED else ComplianceState.COMPLIANCE_STATE_PENDING,
                    if (matches) "" else if (want == null) "ASSIGNMENT_MISSING" else "REQUIRED_APP_PENDING", actual?.let(::applicationValue) ?: "absent")
            }
            for (setting in rules) {
                active()
                try {
                    platform.check(setting)
                    var entry = storage.read().objectValue("policy_journal").optJSONObject(setting.id)
                    if (entry == null) {
                        val baseline = setting.copy(value = platform.capture(setting))
                        entry = JSONObject().put("baseline", baseline.json())
                        persist { it.objectValue("policy_journal").put(setting.id, entry) }
                    }
                    if (setting.kind == PolicyKind.LOCK_TASK && ready.values.any { !it }) {
                        // Missing/drifted apps must never leave an old kiosk lock active.
                        attempt(setting.copy(value = lockTaskValue(emptyList(), "")), entry)
                        results += result(setting.id, ComplianceState.COMPLIANCE_STATE_PENDING, "REQUIRED_APPS_PENDING")
                        continue
                    }
                    if (setting.kind in setOf(PolicyKind.PERMISSION, PolicyKind.MANAGED_CONFIGURATION) && ready[setting.target] != true) throw PolicyPending("REQUIRED_APP_PENDING")
                    val observed = attempt(setting, entry)
                    results += result(setting.id, if (observed == setting.value) ComplianceState.COMPLIANCE_STATE_APPLIED else ComplianceState.COMPLIANCE_STATE_PENDING,
                        if (observed == setting.value) "" else "OBSERVATION_PENDING",
                        if (setting.kind == PolicyKind.MANAGED_CONFIGURATION) ConfigurationValues.digest(ConfigurationValues.read(observed)) else observed)
                } catch (error: Exception) { results += failure(setting.id, error) }
            }
        }
        val report = PolicyReport.newBuilder().setRevision(desired.revision).setSnapshotRevision(snapshot.device.desiredRevision)
            .addAllResults(results.sortedBy { it.ruleId }).build()
        persist { it.put("policy_report", encode(report)) }
        return results.any { it.state == ComplianceState.COMPLIANCE_STATE_PENDING }
    }
}
