// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.app.KeyguardManager
import android.app.admin.DevicePolicyManager
import android.content.ComponentName
import android.content.Context
import java.util.UUID

/** Device Owner effects and platform readback; no host callback can replace their observation. */
internal class AndroidCommandPlatform(private val context: Context) : CommandPlatform {
    private val dpm = context.getSystemService(DevicePolicyManager::class.java)
    private val keyguard = context.getSystemService(KeyguardManager::class.java)
    private val admin = ComponentName(context, KylokDeviceAdminReceiver::class.java)
    companion object {
        private val process = UUID.randomUUID().toString()
        val CAPABILITIES = listOf("command.reboot", "command.lock")
    }
    /** Read the wall clock used by the absolute server deadline. */
    override fun now() = System.currentTimeMillis()
    /** Keep one random identity throughout this process, without persisting it across process death. */
    override fun processID() = process
    /** Check current Device Owner status rather than trusting the last inventory. */
    override fun isOwner() = dpm.isDeviceOwnerApp(context.packageName)
    /** Reuse the boot observation used by managed Lock Task reconciliation. */
    override fun bootCount() = AndroidPolicyPlatform.bootCount(context).toLong()
    /** A swipe lock alone cannot establish a secured device. */
    override fun isSecure() = keyguard.isDeviceSecure
    /** Read Android's effective keyguard state after requesting a lock. */
    override fun isLocked() = keyguard.isDeviceLocked
    /** Request reboot as Device Owner; Android rejects this during an ongoing call. */
    override fun reboot() = dpm.reboot(admin)
    /** Require strong authentication on unlock without changing the device's PIN, password or pattern. */
    override fun lock() = dpm.lockNow()
}
