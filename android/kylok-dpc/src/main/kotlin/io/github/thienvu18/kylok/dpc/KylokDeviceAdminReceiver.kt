// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.app.admin.DeviceAdminReceiver
import android.content.Context
import android.content.Intent

class KylokDeviceAdminReceiver : DeviceAdminReceiver() {
    /** Persist provisioning extras before completing the asynchronous broadcast. Usage: Android sends the profile-provisioning completion callback. */
    override fun onProfileProvisioningComplete(context: Context, intent: Intent) {
        val pending = goAsync()
        Kylok.capture(context, intent, "PROFILE_PROVISIONING_COMPLETE") { pending.finish() }
    }
    /** Capture administrator activation without treating it as proof of Device Owner. Usage: Android invokes this administrator callback. */
    override fun onEnabled(context: Context, intent: Intent) {
        val pending = goAsync()
        Kylok.capture(context, intent, "DEVICE_ADMIN_ENABLED") { pending.finish() }
    }
    /** Record the package and current boot for later managed Lock Task observation. Usage: Android invokes the administrator Lock Task callback. */
    override fun onLockTaskModeEntering(context: Context, intent: Intent, pkg: String) { recordLockTask(context, pkg) }
    /** Clear the recorded locked package before reconciliation. Usage: Android invokes the administrator Lock Task exit callback. */
    override fun onLockTaskModeExiting(context: Context, intent: Intent) { recordLockTask(context, "") }
    /** Persist the current-boot Lock Task observation off the broadcast thread and schedule repair. */
    private fun recordLockTask(context: Context, pkg: String) {
        val pending = goAsync()
        callbacks.execute {
            try {
                AgentStorage.update(context) { it.put("lock_task_package", pkg)
                    .put("lock_task_boot", AndroidPolicyPlatform.bootCount(context)) }
                Kylok.schedule(context)
            } finally { pending.finish() }
        }
    }
    companion object { private val callbacks = java.util.concurrent.Executors.newSingleThreadExecutor() }
}
