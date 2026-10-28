// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import io.github.thienvu18.kylok.proto.v1.*
import org.json.JSONArray
import org.json.JSONObject
import java.util.Base64
import java.util.UUID

internal interface StateAccess {
    /** Read a detached checkpoint suitable for reconciliation. */
    fun read(): JSONObject
    /** Commit the complete state change atomically before external effects. */
    fun update(change: (JSONObject) -> Unit)
}

/** Encode protobuf bytes as base64 for the encrypted JSON checkpoint. */
internal fun encode(message: com.google.protobuf.MessageLite): String = Base64.getEncoder().encodeToString(message.toByteArray())
/** Recover protobuf bytes from their checkpoint encoding. */
internal fun decode(value: String): ByteArray = Base64.getDecoder().decode(value)
/** Create an absent mutable array inside this checkpoint. */
internal fun JSONObject.array(name: String): JSONArray = optJSONArray(name) ?: JSONArray().also { put(name, it) }
/** Create an absent mutable object inside this checkpoint. */
internal fun JSONObject.objectValue(name: String): JSONObject = optJSONObject(name) ?: JSONObject().also { put(name, it) }

/** Commits each request before sending and atomically applies only its matching response. */
internal class SyncCheckpoint(private val storage: StateAccess) {
    /** Replay an in-flight request verbatim or persist the next sequence before sending. */
    fun request(inventory: Inventory): SyncRequest {
        var request: SyncRequest? = null
        storage.update { state ->
            if (state.has("inflight")) request = SyncRequest.parseFrom(decode(state.getString("inflight")))
            else {
                val builder = SyncRequest.newBuilder().setDeviceId(state.getString("device_id"))
                    .setProtocolVersion(BuildConfig.PROTOCOL_VERSION).setSequence(state.optLong("sequence") + 1)
                    .setReportedRevision(state.optLong("revision")).setInventory(inventory)
                if (state.has("network_report")) builder.networkReport = NetworkReport.parseFrom(decode(state.getString("network_report")))
                if (state.has("policy_report")) builder.policyReport = PolicyReport.parseFrom(decode(state.getString("policy_report")))
                val local = state.objectValue("operations")
                for (id in local.keys().asSequence().sorted()) {
                    val record = local.getJSONObject(id)
                    if (!record.optBoolean("terminal") && !record.optBoolean("superseded")) builder.addRetainedOperationIds(id)
                }
                require(builder.retainedOperationIdsCount <= 64)
                val commandReports = state.objectValue("command_reports")
                require(commandReports.length() <= 16)
                for (id in commandReports.keys().asSequence().sorted()) builder.addCommandReports(CommandReport.parseFrom(decode(commandReports.getString(id))))
                val reports = state.array("reports")
                // The server accepts at most 64 reports in one immutable sync request; retain the rest.
                for (index in 0 until minOf(64, reports.length())) builder.addReports(OperationReport.parseFrom(decode(reports.getString(index))))
                request = builder.build()
                state.put("inflight", encode(request!!))
            }
        }
        return requireNotNull(request)
    }

    /** Authenticate response identity/revisions and atomically acknowledge reports and supersede work. */
    fun accept(response: SyncResponse) {
        storage.update { state ->
            val pending = SyncRequest.parseFrom(decode(state.getString("inflight")))
            require(response.protocolVersion == BuildConfig.PROTOCOL_VERSION && response.sequence == pending.sequence && response.hasDevice())
            require(response.device.id == state.getString("device_id") && response.device.scopeId == state.getString("scope_id"))
            require(response.device.desiredRevision >= state.optLong("revision") && response.device.state == DeviceState.DEVICE_STATE_MANAGED)
            if (response.hasDesiredPolicy()) require(response.desiredPolicy.hasPolicy() && response.desiredPolicy.revision > 0 &&
                response.desiredPolicy.revision <= response.device.desiredRevision)
            if (state.has("snapshot")) {
                val previous = SyncResponse.parseFrom(decode(state.getString("snapshot")))
                if (previous.hasDesiredPolicy()) {
                    require(response.hasDesiredPolicy() && response.desiredPolicy.revision >= previous.desiredPolicy.revision)
                    if (response.desiredPolicy.revision == previous.desiredPolicy.revision) require(response.desiredPolicy == previous.desiredPolicy)
                }
            }
            if (response.hasDesiredNetworkPolicy()) require(response.desiredNetworkPolicy.hasPolicy() && response.desiredNetworkPolicy.revision > 0 && response.desiredNetworkPolicy.revision <= response.device.desiredRevision)
            if (state.has("snapshot")) {
                val prior = SyncResponse.parseFrom(decode(state.getString("snapshot")))
                if (prior.hasDesiredNetworkPolicy()) {
                    require(response.hasDesiredNetworkPolicy() && response.desiredNetworkPolicy.revision >= prior.desiredNetworkPolicy.revision)
                    if (response.desiredNetworkPolicy.revision == prior.desiredNetworkPolicy.revision) require(response.desiredNetworkPolicy == prior.desiredNetworkPolicy)
                }
            }
            val acknowledged = response.receiptsList.map { it.reportId }.toSet()
            val remaining = JSONArray()
            val reports = state.array("reports")
            for (index in 0 until reports.length()) {
                val report = OperationReport.parseFrom(decode(reports.getString(index)))
                if (report.id !in acknowledged) remaining.put(reports.getString(index))
            }
            state.put("reports", remaining).put("sequence", response.sequence)
                .put("revision", response.device.desiredRevision).put("snapshot", encode(response))
            state.remove("inflight")
            val commands = state.objectValue("commands")
            val commandReports = state.objectValue("command_reports")
            val sentCommandReports = pending.commandReportsList.map { it.commandId }.toSet()
            require(response.commandReceiptsList.size <= 16 && response.commandReceiptsList.all { it in sentCommandReports })
            for (id in response.commandReceiptsList) { commandReports.remove(id); commands.remove(id) }
            require(response.commandsCount <= 1)
            val delivered = response.commandsList.map { it.id }.toSet()
            for (id in commands.keys().asSequence().toList()) {
                val record = commands.getJSONObject(id)
                // Omission stops work that has not crossed its pre-effect checkpoint. Armed work is only observed.
                if (id !in delivered && record.optString("stage") !in setOf("armed", "terminal")) commands.remove(id)
            }
            for (command in response.commandsList) {
                require(command.id.matches(Regex("[A-Za-z0-9_-]{43}")) && command.dispatchToken.matches(Regex("[0-9a-f]{48}")) &&
                    command.kind in setOf("REBOOT", "LOCK") && command.deadlineUnixMillis > 0 && command.id !in response.commandReceiptsList)
                val record = commands.optJSONObject(command.id) ?: JSONObject()
                if (record.has("command")) require(Command.parseFrom(decode(record.getString("command"))) == command)
                record.put("command", encode(command)); commands.put(command.id, record)
            }
            val operations = state.objectValue("operations")
            val desired = response.desiredApplicationsList.associateBy { it.artifact.packageName }
            // A newer desired package revision supersedes local pending work.
            operations.keys().asSequence().toList().forEach { id ->
                val local = operations.getJSONObject(id)
                val op = Operation.parseFrom(decode(local.getString("operation")))
                val want = desired[op.artifact.packageName]
                if (want == null || want.artifact.sha256 != op.artifact.sha256 || want.revision != op.revision) local.put("superseded", true)
            }
            val stopped = response.stoppedOperationIdsList.toSet()
            require(stopped.size <= 64 && stopped.all { it in pending.retainedOperationIdsList })
            for (id in stopped) operations.optJSONObject(id)?.put("superseded", true)
            for (op in response.operationsList) {
                require(op.id !in stopped && op.notBeforeUnixMillis >= 0 && op.notBeforeUnixMillis < op.deadlineUnixMillis)
                require(op.id.matches(Regex("[A-Za-z0-9_-]{43}")) && op.leaseId.matches(Regex("[0-9a-f]{48}")))
                val want = desired[op.artifact.packageName]
                require(want != null && want.revision == op.revision && want.artifact == op.artifact)
                val local = operations.optJSONObject(op.id) ?: JSONObject()
                val previous = local.optString("operation")
                if (previous.isNotEmpty()) {
                    val old = Operation.parseFrom(decode(previous))
                    require(old.artifact == op.artifact && old.revision == op.revision && old.notBeforeUnixMillis == op.notBeforeUnixMillis && old.deadlineUnixMillis == op.deadlineUnixMillis)
                    if (old.leaseId != op.leaseId) local.remove("reported")
                }
                local.put("operation", encode(op)); operations.put(op.id, local)
            }
            val currentIDs = response.operationsList.map { it.id }.toSet()
            val pendingIDs = (0 until remaining.length()).map { OperationReport.parseFrom(decode(remaining.getString(it))).operationId }.toSet()
            operations.keys().asSequence().toList().forEach { id ->
                val local = operations.getJSONObject(id)
                if (id !in currentIDs && id !in pendingIDs && (local.optBoolean("terminal") || local.optBoolean("superseded"))) operations.remove(id)
            }
        }
    }
}

/** Coalesce progress while retaining reports referenced by an immutable in-flight request. */
internal fun queueReport(state: JSONObject, local: JSONObject, op: Operation, status: OperationState,
    code: String = "", message: String = "", observed: InstalledApplication? = null) {
    val key = "${op.leaseId}:$status:$code:${local.optLong("bytes")}:${observed?.versionCode}"
    if (local.optString("reported") == key) return
    val report = OperationReport.newBuilder().setId(UUID.randomUUID().toString()).setOperationId(op.id)
        .setLeaseId(op.leaseId).setSequence(local.optLong("report_sequence") + 1).setState(status)
        .setDownloadedBytes(local.optLong("bytes")).setErrorCode(code.take(128)).setErrorMessage(message.take(1024))
    if (observed != null) report.observed = observed
    val inflight = if (state.has("inflight")) SyncRequest.parseFrom(decode(state.getString("inflight"))).reportsList.map { it.id }.toSet() else emptySet()
    val outbox = state.array("reports")
    val kept = JSONArray()
    for (index in 0 until outbox.length()) {
        val previous = OperationReport.parseFrom(decode(outbox.getString(index)))
        if (previous.operationId != op.id || previous.id in inflight) kept.put(outbox.getString(index))
    }
    kept.put(encode(report.build())); state.put("reports", kept)
    local.put("report_sequence", report.sequence).put("reported", key)
}
