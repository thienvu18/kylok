// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.content.Context
import android.os.Build
import androidx.work.Worker
import androidx.work.WorkerParameters
import io.github.thienvu18.kylok.proto.v1.Inventory
import io.github.thienvu18.kylok.proto.v1.Operation
import java.io.IOException
import io.github.thienvu18.kylok.proto.v1.DeviceState
import io.grpc.Status
import io.grpc.StatusRuntimeException

class EnrollmentWorker(context: Context, params: WorkerParameters) : Worker(context, params) {
    @Volatile private var runningThread: Thread? = null
    /** Serialize agent runs and expose the active thread for WorkManager cancellation. Usage: WorkManager invokes this callback for the SDK’s scheduled work. */
    override fun doWork(): Result = synchronized(runLock) {
        Kylok.initialize()
        runningThread = Thread.currentThread()
        try { if (isStopped) Result.retry() else runAgent() } finally { runningThread = null }
    }
    /** Interrupt network/download/policy work when WorkManager stops this worker. Usage: invoked by WorkManager cancellation. */
    override fun onStopped() { runningThread?.interrupt(); super.onStopped() }

    /** Repair cached policy, perform bound enrollment/sync, reconcile effects and classify retryable failures. */
    private fun runAgent(): Result {
        try {
            check(Kylok.owner(applicationContext)) { "Device Owner required" }
            var state = AgentStorage.read(applicationContext)
            if (!state.has("server")) return Result.success()
            if (state.optString("lifecycle") == "REVOKED") return Result.failure()
            val inventory = Inventory.newBuilder().setApiLevel(Build.VERSION.SDK_INT)
                .setModel("${Build.MANUFACTURER} ${Build.MODEL}").setBuildFingerprint(Build.FINGERPRINT)
                .setAgentVersion(BuildConfig.SDK_VERSION).setDeviceOwner(true).addCapabilities("install_apk")
                .addCapabilities("durable_sync").addAllCapabilities(AndroidPolicyPlatform.CAPABILITIES)
                .addAllSupportedAbis(Build.SUPPORTED_ABIS.toList()).build()
            val access = AgentStorage.access(applicationContext)
            val policy = PolicyEngine(access, AndroidPolicyPlatform(applicationContext, access))
            // Previously authenticated desired policies remain effective on an
            // offline reboot; network delivery is not a prerequisite for repair.
            if (state.has("credential")) policy.run()
            check(Kylok.lanGranted(applicationContext)) { "LAN permission required" }
            AgentClient.connect(state.getString("server")).use { client ->
                if (!state.has("credential")) {
                    val keys = AgentStorage.identity()
                    AgentStorage.update(applicationContext) { it.put("lifecycle", "ENROLLING") }
                    val response = client.enroll(state.getString("token"), state.getString("attempt"),
                        keys.public.encoded, inventory)
                    val credential = EnrollmentCrypto.open(keys.private, response, state.getString("attempt"))
                    // Commit identity and protected credential together before the first authenticated RPC.
                    AgentStorage.update(applicationContext) {
                        it.put("device_id", response.device.id).put("scope_id", response.device.scopeId)
                            .put("credential", credential)
                        it.remove("token")
                    }
                    state = AgentStorage.read(applicationContext)
                }
                val platform = AndroidInstallPlatform(applicationContext, access)
                val checkpoint = SyncCheckpoint(access)
                var pending = false
                // Bound one worker run: deliver desired state, reconcile it, then send resulting observations.
                repeat(3) {
                    if (isStopped) throw IOException("Cancelled")
                    val current = access.read()
                    val builder = inventory.toBuilder()
                    val names = mutableSetOf(applicationContext.packageName)
                    if (current.has("snapshot")) {
                        val snapshot = io.github.thienvu18.kylok.proto.v1.SyncResponse.parseFrom(decode(current.getString("snapshot")))
                        names.addAll(snapshot.desiredApplicationsList.map { it.artifact.packageName })
                    }
                    val operations = current.objectValue("operations")
                    for (id in operations.keys()) {
                        val op = Operation.parseFrom(decode(operations.getJSONObject(id).getString("operation")))
                        names += op.artifact.packageName
                    }
                    for (name in names.take(128)) platform.installed(name)?.let { builder.addApplications(it) }
                    val request = checkpoint.request(builder.build())
                    val response = client.sync(request, current.getString("credential"))
                    checkpoint.accept(response)
                    pending = InstallEngine(access, platform).run()
                    pending = policy.run() || pending
                    AgentStorage.update(applicationContext) {
                        it.put("lifecycle", "MANAGED").put("result", "Authenticated sync revision ${response.device.desiredRevision}")
                    }
                    Kylok.notifyStatus(applicationContext)
                }
                if (pending || access.read().array("reports").length() > 0) return Result.retry()
            }
            return Result.success()
        } catch (error: StatusRuntimeException) {
            val code = error.status.code
            val retry = code in setOf(Status.Code.UNAVAILABLE, Status.Code.DEADLINE_EXCEEDED,
                Status.Code.RESOURCE_EXHAUSTED, Status.Code.ABORTED, Status.Code.INTERNAL, Status.Code.CANCELLED)
            runCatching {
                AgentStorage.update(applicationContext) {
                    it.put("lifecycle", if (code == Status.Code.PERMISSION_DENIED) "REVOKED" else if (retry) "ENROLLING" else "ERROR")
                        .put("result", "RPC ${code.name}")
                }
            }
            Kylok.notifyStatus(applicationContext)
            return if (retry) Result.retry() else Result.failure()
        } catch (error: IOException) {
            Kylok.notifyStatus(applicationContext)
            return Result.retry()
        } catch (error: Exception) {
            runCatching {
                AgentStorage.update(applicationContext) {
                    it.put("lifecycle", "ERROR").put("result", error.javaClass.simpleName)
                }
            }
            Kylok.notifyStatus(applicationContext)
            return Result.failure()
        }
    }
    companion object { private val runLock = Any() }
}
