// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import io.github.thienvu18.kylok.proto.v1.*
import java.io.IOException

internal class InstallRejected(val reason: String) : Exception(reason)
internal interface InstallPlatform {
    /** Read wall time for the server operation deadline. */
    fun now(): Long
    /** Confirm Device Owner before privileged installation. */
    fun isOwner(): Boolean
    /** Observe installed package/version/signers, returning null when absent. */
    fun installed(packageName: String): InstalledApplication?
    /** Stage resumable verified bytes and checkpoint durable progress. */
    fun download(op: Operation, progress: (Long) -> Unit)
    /** Reject an APK whose content, metadata or platform compatibility differs from its assignment. */
    fun verify(op: Operation)
    /** Create an installer session whose ID is journaled before subsequent effects. */
    fun createSession(op: Operation): Int
    /** Write and fsync the verified base APK into an existing session. */
    fun writeSession(id: Int, op: Operation)
    /** Check whether an already journaled installer session survived. */
    fun sessionExists(id: Int): Boolean
    /** Detect a session that crossed commit so recovery cannot install twice. */
    fun sessionSealed(id: Int): Boolean
    /** Commit the session with a callback bound to its operation and session IDs. */
    fun commit(id: Int, operationID: String)
    /** Release a session that is failed, superseded or no longer retained. */
    fun abandon(id: Int)
    /** Remove only Kylok staging and uncommitted sessions absent from retained work. */
    fun cleanup(retained: Set<String>, sessions: Set<Int>)
}

/** Require package, exact version and signer equality before reporting observed success. */
internal fun installedMatches(observed: InstalledApplication?, artifact: Artifact): Boolean = observed != null &&
    observed.packageName == artifact.packageName && observed.versionCode == artifact.versionCode &&
    observed.signingCertificatesList.sorted() == artifact.signingCertificatesList.sorted()

/** Android effects are reconciled against durable checkpoints; callbacks are hints, not proof. */
internal class InstallEngine(private val storage: StateAccess, private val platform: InstallPlatform) {
    /** Reconcile journaled operations against installed state; return whether work remains pending. */
    fun run(): Boolean {
        val operations = storage.read().objectValue("operations")
        var pending = false
        for (id in operations.keys().asSequence().toList()) {
            val local = operations.getJSONObject(id)
            val op = Operation.parseFrom(decode(local.getString("operation")))
            /** Reload and atomically update this operation with the shared outbox. */
            fun change(block: (org.json.JSONObject, org.json.JSONObject) -> Unit) = storage.update { state ->
                block(state, state.objectValue("operations").getJSONObject(id))
            }
            if (local.optBoolean("superseded")) {
                if (local.has("session")) platform.abandon(local.getInt("session"))
                continue
            }
            try {
                if (!platform.isOwner()) throw InstallRejected("DEVICE_OWNER_REQUIRED")
                if (platform.now() >= op.deadlineUnixMillis) throw InstallRejected("DEADLINE_EXCEEDED")
                val observed = platform.installed(op.artifact.packageName)
                if (installedMatches(observed, op.artifact)) {
                    change { state, record -> record.put("terminal", true); queueReport(state, record, op, OperationState.OPERATION_STATE_SUCCEEDED, observed = observed) }
                    continue
                }
                if (local.optBoolean("terminal")) {
                    change { state, record -> queueReport(state, record, op, OperationState.OPERATION_STATE_FAILED, record.optString("failure", "INSTALL_FAILED")) }
                    continue
                }
                val callback = local.optString("callback")
                if (callback == "success") throw InstallRejected("INSTALLED_STATE_MISMATCH")
                if (callback == "failure") throw InstallRejected(local.optString("callback_code", "INSTALL_FAILED"))
                if (callback == "waiting") {
                    change { state, record -> queueReport(state, record, op, OperationState.OPERATION_STATE_WAITING_USER, "USER_ACTION_REQUIRED") }
                    pending = true; continue
                }
                if (observed != null && observed.versionCode > op.artifact.versionCode) throw InstallRejected("DOWNGRADE_REJECTED")
                var session = local.optInt("session", -1)
                if (session >= 0 && !platform.sessionExists(session)) {
                    if (local.optString("stage") in setOf("committing", "committed")) throw InstallRejected("SESSION_LOST")
                    session = -1
                }
                if (session < 0) {
                    platform.download(op) { bytes -> change { _, record -> record.put("bytes", bytes) } }
                    platform.verify(op)
                    session = platform.createSession(op)
                    val created = session
                    // Persist the session ID before writing or committing external work.
                    change { _, record -> record.put("session", created).put("stage", "created") }
                }
                val record = storage.read().objectValue("operations").getJSONObject(id)
                if (record.optString("stage") !in setOf("committing", "committed")) {
                    platform.verify(op)
                    platform.writeSession(session, op)
                    change { _, current -> current.put("stage", "committing") }
                }
                // A sealed session already crossed commit; never create a second install.
                if (!platform.sessionSealed(session)) platform.commit(session, id)
                change { state, current -> current.put("stage", "committed"); queueReport(state, current, op, OperationState.OPERATION_STATE_RUNNING) }
                pending = true
            } catch (error: InstallRejected) {
                change { state, record -> record.put("terminal", true).put("failure", error.reason); queueReport(state, record, op, OperationState.OPERATION_STATE_FAILED, error.reason) }
                if (local.has("session")) platform.abandon(local.getInt("session"))
            } catch (error: IOException) {
                change { state, record -> queueReport(state, record, op, OperationState.OPERATION_STATE_RUNNING, "TRANSPORT_RETRY", error.javaClass.simpleName) }
                pending = true
            } catch (error: SecurityException) {
                change { state, record -> record.put("terminal", true).put("failure", "PLATFORM_PERMISSION_DENIED"); queueReport(state, record, op, OperationState.OPERATION_STATE_FAILED, "PLATFORM_PERMISSION_DENIED") }
            } catch (error: IllegalArgumentException) {
                change { state, record -> record.put("terminal", true).put("failure", "INVALID_ARTIFACT"); queueReport(state, record, op, OperationState.OPERATION_STATE_FAILED, "INVALID_ARTIFACT") }
            }
        }
        val current = storage.read().objectValue("operations")
        val retained = mutableSetOf<String>(); val sessions = mutableSetOf<Int>()
        for (id in current.keys()) {
            val record = current.getJSONObject(id)
            if (!record.optBoolean("terminal") && !record.optBoolean("superseded")) {
                retained += id
                if (record.has("session")) sessions += record.getInt("session")
            }
        }
        platform.cleanup(retained, sessions)
        return pending
    }
}
