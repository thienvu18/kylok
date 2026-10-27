// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.app.admin.DevicePolicyManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.PersistableBundle
import androidx.work.*
import java.net.URI
import java.util.UUID
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/** Experimental device agent. Components initialize without opening the host Activity. */
object Kylok {
    /** SDK identity is independent of the Android host's application version. */
    val sdkVersion: String get() = BuildConfig.SDK_VERSION
    /** Wire identity for paired SDKs; matching this alone does not guarantee development compatibility. */
    val protocolVersion: Int get() = BuildConfig.PROTOCOL_VERSION
    private val listeners = StatusListeners()
    private val configuration = HostConfiguration()
    internal const val LAN_PERMISSION = "android.permission.ACCESS_LOCAL_NETWORK"
    private val executor = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())
    /** Configure scheduling before the first component/worker starts; equal repeats are idempotent.
     * Different options after work starts throw IllegalStateException. Options are process-local;
     * call this in Application.onCreate on every start. Listeners own their returned registration;
     * WorkManager owns scheduled workers and hosts must not shut down SDK executors.
     * Usage: `Kylok.configure(this, HostOptions(syncIntervalMinutes = 30))`.
     */
    fun configure(context: Context, options: HostOptions) {
        require(context.applicationContext != null)
        configuration.configure(options)
    }
    /** Freeze options for platform-triggered starts, independently of an opened Activity. */
    internal fun initialize() = configuration.start()
    /** Query effective ownership before any privileged agent work. */
    internal fun owner(context: Context) =
        context.getSystemService(DevicePolicyManager::class.java).isDeviceOwnerApp(context.packageName)
    /** Older Android versions do not require the runtime LAN permission. */
    internal fun lanGranted(context: Context) = Build.VERSION.SDK_INT < 37 ||
        context.checkSelfPermission(LAN_PERMISSION) == PackageManager.PERMISSION_GRANTED

    /** Persist and bind provisioning extras before scheduling enrollment or returning to Setup Wizard. */
    internal fun capture(context: Context, intent: Intent, callback: String, done: (Boolean) -> Unit) {
        val extras = intent.getParcelableExtra(
            DevicePolicyManager.EXTRA_PROVISIONING_ADMIN_EXTRAS_BUNDLE, PersistableBundle::class.java)
        val app = context.applicationContext
        initialize()
        executor.execute {
            val success = runCatching {
                AgentStorage.update(app) { state ->
                    state.put("callback", callback)
                    val server = extras?.getString("kylok.server")
                    val token = extras?.getString("kylok.enrollment_token")
                    if (server != null || token != null) {
                        require(server != null && token != null && token.matches(Regex("[A-Za-z0-9_-]{43}")))
                        val uri = URI(server)
                        require(uri.scheme in setOf("http", "https") && !uri.host.isNullOrEmpty() && uri.rawUserInfo == null &&
                            uri.rawQuery == null && uri.rawFragment == null && uri.rawPath.isNullOrEmpty())
                        if (!state.has("attempt")) {
                            state.put("server", server).put("token", token).put("attempt", UUID.randomUUID().toString())
                                .put("lifecycle", "PROVISIONING")
                        } else {
                            require(state.getString("server") == server)
                            if (state.has("token")) require(state.getString("token") == token)
                        }
                    }
                    if (owner(app) && state.optString("lifecycle") == "PROVISIONING") state.put("lifecycle", "DEVICE_OWNER")
                }
            }.isSuccess
            if (success) schedule(app)
            main.post { done(success) }
        }
    }
    /** Schedule deduplicated immediate and periodic repair, including unvalidated LANs. */
    internal fun schedule(context: Context) {
        // Cached policy repair needs neither a network connection nor LAN access.
        if (!owner(context)) return
        val options = initialize()
        val request = OneTimeWorkRequestBuilder<EnrollmentWorker>()
            // CONNECTED requires a validated network on current Android. A LAN with
            // WAN blocked can be unvalidated; attempt the endpoint and retry transport errors.
            // Jitter spreads simultaneous boot traffic; backoff and the 15-minute period meet WorkManager minima.
            .setInitialDelay((0..options.wakeJitterSeconds).random().toLong(), TimeUnit.SECONDS)
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, options.retryBackoffSeconds, TimeUnit.SECONDS).build()
        WorkManager.getInstance(context).enqueueUniqueWork("kylok.enrollment.v1", ExistingWorkPolicy.KEEP, request)
        val periodic = PeriodicWorkRequestBuilder<EnrollmentWorker>(options.syncIntervalMinutes, TimeUnit.MINUTES)
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, options.retryBackoffSeconds, TimeUnit.SECONDS).build()
        WorkManager.getInstance(context).enqueueUniquePeriodicWork("kylok.sync.v1", ExistingPeriodicWorkPolicy.KEEP, periodic)
    }
    /** Schedule reconciliation without rotating credentials. Usage: `Kylok.retry(context)` from a refresh action. */
    fun retry(context: Context) { schedule(context.applicationContext) }

    /** Read one sanitized snapshot on a worker, delivered on the main thread.
     * Usage: `Kylok.snapshot(context) { status -> render(status) }`.
     */
    fun snapshot(context: Context, receive: (AgentStatus) -> Unit) {
        val app = context.applicationContext
        executor.execute {
            val value = readStatus(app)
            main.post { receive(value) }
        }
    }
    /** Optional, read-only notifications after durable worker changes. Close the
     * registration with the host lifecycle. Earlier events are not replayed.
     * Usage: `val registration = Kylok.observe(StatusListener { render(it) })`; close on host stop.
     */
    fun observe(listener: StatusListener): java.io.Closeable = listeners.add(listener)
    /** Read protected state only when a host subscribes, and deliver after durable worker changes. */
    internal fun notifyStatus(context: Context) {
        if (!listeners.hasListeners()) return
        val app = context.applicationContext
        executor.execute { val value = readStatus(app); main.post { listeners.deliver(value) } }
    }
    /** Convert storage failures into sanitized diagnostics without exposing protected state. */
    private fun readStatus(context: Context): AgentStatus = try {
        statusValue(AgentStorage.read(context), owner(context), lanGranted(context))
    } catch (error: Exception) {
        AgentStatus(sdkVersion, protocolVersion, "ERROR", null, null, runCatching { owner(context) }.getOrDefault(false),
            runCatching { lanGranted(context) }.getOrDefault(false), 0,
            "Cannot read protected state: ${error.javaClass.simpleName}", emptyList())
    }

    /** Open the first pending Android installation confirmation from a host user action.
     * Usage: `Kylok.confirmPendingInstall(activity)` when a snapshot reports `requiresUserAction`.
     */
    fun confirmPendingInstall(context: Context) {
        val app = context.applicationContext
        executor.execute {
            val operations = AgentStorage.read(app).objectValue("operations")
            val action = operations.keys().asSequence().map { operations.getJSONObject(it) }
                .firstOrNull { !it.optBoolean("superseded") && !it.optBoolean("terminal") && it.has("confirmation") }
                ?.getString("confirmation") ?: return@execute
            main.post { context.startActivity(Intent.parseUri(action, Intent.URI_INTENT_SCHEME).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) }
        }
    }

    /** Return formatted local diagnostics on the main thread; credentials and keys are excluded.
     * Usage: `Kylok.status(context) { textView.text = it }`. Use `snapshot` for structured UI data.
     */
    fun status(context: Context, receive: (String) -> Unit) {
        val app = context.applicationContext
        executor.execute {
            val report = runCatching {
                val state = AgentStorage.read(app)
                "Device Owner: ${owner(app)}\nAPI: ${Build.VERSION.SDK_INT}\n" +
                    "Model: ${Build.MANUFACTURER} ${Build.MODEL}\nBuild: ${Build.FINGERPRINT}\n" +
                    "LAN permission: ${lanGranted(app)}\nCallback: ${state.optString("callback", "not received")}\n" +
                    "State: ${state.optString("lifecycle", "UNMANAGED")}\n" +
                    "Device ID: ${state.optString("device_id", "not enrolled")}\n" +
                    "Last result: ${state.optString("result", "pending / not configured") }" +
                    if (state.has("policy_report")) {
                        val policy = io.github.thienvu18.kylok.proto.v1.PolicyReport.parseFrom(decode(state.getString("policy_report")))
                        "\nPolicy revision: ${policy.revision}\n" + policy.resultsList.joinToString("\n") {
                            "${it.ruleId}: ${it.state.name.removePrefix("COMPLIANCE_STATE_")}${if (it.reason.isEmpty()) "" else " (${it.reason})"}"
                        }
                    } else ""
            }.getOrElse { "Cannot read protected agent state: ${it.javaClass.simpleName}" }
            main.post { receive(report) }
        }
    }
}
