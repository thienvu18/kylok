// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import java.util.concurrent.Executors

class InstallResultReceiver : BroadcastReceiver() {
    /** Persist only a callback matching the journaled session, then schedule observed reconciliation. Usage: Android invokes the explicit PackageInstaller result PendingIntent. */
    override fun onReceive(context: Context, intent: Intent) {
        Kylok.initialize()
        val pending = goAsync()
        executor.execute {
            try {
                val id = intent.getStringExtra("operation_id") ?: return@execute
                val session = intent.getIntExtra("session_id", -1)
                val result = intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)
                var confirmation: Intent? = null
                AgentStorage.update(context) { state ->
                    val local = state.objectValue("operations").optJSONObject(id) ?: return@update
                    if (session != local.optInt("session", -2) || local.optBoolean("superseded")) return@update
                    when (result) {
                        PackageInstaller.STATUS_SUCCESS -> { local.put("callback", "success"); local.remove("confirmation") }
                        PackageInstaller.STATUS_PENDING_USER_ACTION -> {
                            confirmation = intent.getParcelableExtra(Intent.EXTRA_INTENT, Intent::class.java)
                            local.put("callback", "waiting")
                            confirmation?.let { local.put("confirmation", it.toUri(Intent.URI_INTENT_SCHEME)) }
                        }
                        else -> { local.put("callback", "failure").put("callback_code", "PACKAGE_INSTALLER_$result"); local.remove("confirmation") }
                    }
                }
                confirmation?.let { showConfirmation(context, session, it) }
                Kylok.schedule(context)
                if (result == PackageInstaller.STATUS_SUCCESS) LauncherRecovery.recover(context)
            } finally { pending?.finish() }
        }
    }
    /** Offer the OS confirmation without losing the persisted fallback when notifications are denied. */
    private fun showConfirmation(context: Context, session: Int, intent: Intent) {
        val manager = context.getSystemService(NotificationManager::class.java)
            ?: context.getSystemService(Context.NOTIFICATION_SERVICE) as? NotificationManager ?: return
        manager.createNotificationChannel(NotificationChannel("kylok-installs", "Application installation", NotificationManager.IMPORTANCE_DEFAULT))
        val action = PendingIntent.getActivity(context, session, intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
        val notification = runCatching {
            Notification.Builder(context, "kylok-installs").apply {
                setSmallIcon(android.R.drawable.stat_sys_download_done)
                setContentTitle("Installation needs confirmation")
                setContentText("Open to review the Android installation prompt.")
                setContentIntent(action)
                setAutoCancel(true)
            }.build()
        }.getOrNull()
        // Without notification permission the pending action remains available through Kylok.confirmPendingInstall.
        if (notification != null) {
            runCatching { manager.notify(session, notification) }
        }
    }
    companion object {
        /** Background executor for processing broadcast results without blocking the main thread. */
        internal var executor: java.util.concurrent.Executor = Executors.newSingleThreadExecutor()
    }
}
