// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import io.github.thienvu18.kylok.proto.v1.*
import org.json.JSONObject
import java.io.Closeable
import java.util.concurrent.CopyOnWriteArraySet

/** Sanitized local observation. Usage: render the result of `Kylok.snapshot(context) { ... }`.
 * App observations describe the last authenticated sync; credentials, enrollment QR and keys are excluded.
 */
data class AgentStatus(
    val sdkVersion: String,
    val protocolVersion: Int,
    val lifecycle: String,
    val deviceId: String?,
    val scopeId: String?,
    val deviceOwner: Boolean,
    val lanPermissionGranted: Boolean,
    val desiredRevision: Long,
    val lastResult: String,
    val policies: List<PolicyStatus>,
    val serverOrigin: String? = null,
    val lastSyncUnixMillis: Long = 0,
    val syncSequence: Long = 0,
    val pendingReports: Int = 0,
    val applications: List<ApplicationStatus> = emptyList(),
    val operations: List<InstallationStatus> = emptyList()
)
/** One local rule result. Usage: render each item in `AgentStatus.policies`, including its reason. */
data class PolicyStatus(val ruleId: String, val state: String, val reason: String, val observedValue: String = "")
/** Desired and last-synced versions remain distinct. Usage: render `AgentStatus.applications`. */
data class ApplicationStatus(val packageName: String, val desiredVersion: Long, val observedVersion: Long?, val state: String, val reason: String)
/** Durable install progress. Usage: offer OS confirmation when `requiresUserAction` is true. */
data class InstallationStatus(val id: String, val packageName: String, val version: Long, val stage: String,
    val downloadedBytes: Long, val sizeBytes: Long, val error: String, val requiresUserAction: Boolean)
/** Read-only main-thread notifications. Usage: `Kylok.observe(StatusListener { render(it) })`. */
fun interface StatusListener {
    /** Render a local observation and return promptly. Usage: implement this callback; close its registration with the host lifecycle. */
    fun onStatus(status: AgentStatus)
}

/** Snapshot delivery is supplied by Android's main thread. Closed registrations
 * do not receive queued deliveries. Host exceptions cannot affect agent work. */
internal class StatusListeners {
    private class Registration(val listener: StatusListener) { @Volatile var active = true }
    private val listeners = CopyOnWriteArraySet<Registration>()
    /** Release stopped host listeners explicitly instead of retaining their Activities. */
    fun add(listener: StatusListener): Closeable {
        val registration = Registration(listener)
        listeners.add(registration)
        return Closeable { registration.active = false; listeners.remove(registration) }
    }
    /** Avoid protected storage reads when no host observes status. */
    fun hasListeners() = listeners.isNotEmpty()
    /** Skip closed registrations and isolate host callback failures. */
    fun deliver(status: AgentStatus) { listeners.forEach { if (it.active) runCatching { it.listener.onStatus(status) } } }
}

/** Convert protected checkpoints to public read-only data, marking superseded observations pending. */
internal fun statusValue(state: JSONObject, owner: Boolean, lan: Boolean): AgentStatus {
    val snapshot = if (state.has("snapshot")) SyncResponse.parseFrom(decode(state.getString("snapshot"))) else null
    val report = if (state.has("policy_report")) PolicyReport.parseFrom(decode(state.getString("policy_report"))) else null
    val currentPolicy = report != null && report.snapshotRevision == state.optLong("revision")
    val policies = report?.resultsList?.map {
        PolicyStatus(it.ruleId, if (currentPolicy) it.state.name.removePrefix("COMPLIANCE_STATE_") else "PENDING",
            if (currentPolicy) it.reason else "AWAITING_RECONCILIATION", it.observedValue)
    } ?: emptyList()
    val applications = snapshot?.desiredApplicationsList?.map { desired ->
        val observed = snapshot.device.inventory.applicationsList.firstOrNull { it.packageName == desired.artifact.packageName }
        val compliance = snapshot.complianceList.firstOrNull { it.packageName == desired.artifact.packageName }
        ApplicationStatus(desired.artifact.packageName, desired.artifact.versionCode, observed?.versionCode,
            compliance?.state?.name?.removePrefix("COMPLIANCE_STATE_") ?: "PENDING", compliance?.reason ?: "NOT_OBSERVED")
    } ?: emptyList()
    val locals = state.objectValue("operations")
    val operations = locals.keys().asSequence().map { id ->
        val local = locals.getJSONObject(id)
        val op = Operation.parseFrom(decode(local.getString("operation")))
        val stage = when {
            local.optBoolean("superseded") -> "SUPERSEDED"
            local.optBoolean("terminal") -> if (local.has("failure")) "FAILED" else "SUCCEEDED"
            local.optString("callback") == "waiting" -> "WAITING_USER"
            else -> local.optString("stage", "QUEUED").uppercase()
        }
        InstallationStatus(id, op.artifact.packageName, op.artifact.versionCode, stage, local.optLong("bytes"), op.artifact.sizeBytes,
            local.optString("failure", local.optString("callback_code")), !local.optBoolean("terminal") && !local.optBoolean("superseded") && local.has("confirmation"))
    }.toList()
    return AgentStatus(BuildConfig.SDK_VERSION, BuildConfig.PROTOCOL_VERSION, state.optString("lifecycle", "UNMANAGED"),
        state.optString("device_id").takeIf { it.isNotEmpty() }, state.optString("scope_id").takeIf { it.isNotEmpty() },
        owner, lan, state.optLong("revision"), state.optString("result", "pending / not configured"), policies,
        state.optString("server").takeIf { it.isNotEmpty() }, snapshot?.device?.lastSeenAtUnixMillis ?: 0,
        state.optLong("sequence"), state.array("reports").length(), applications, operations)
}
