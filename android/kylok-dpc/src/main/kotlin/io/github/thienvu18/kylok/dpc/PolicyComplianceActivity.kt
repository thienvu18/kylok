// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.app.Activity
import android.app.admin.DevicePolicyManager
import android.content.ComponentName
import android.os.Bundle
import android.widget.TextView

class PolicyComplianceActivity : Activity() {
    /** Accept only the owner’s policy-compliance intent and persist extras before finishing setup. Usage: Setup Wizard starts this merged provisioning Activity. */
    override fun onCreate(state: Bundle?) {
        super.onCreate(state)
        setResult(RESULT_CANCELED)
        if (intent.action != DevicePolicyManager.ACTION_ADMIN_POLICY_COMPLIANCE ||
            !Kylok.owner(this)) {
            finish()
            return
        }
        Kylok.capture(this, intent, "ADMIN_POLICY_COMPLIANCE") { success ->
            if (success) prepareLAN() else finish()
        }
    }

    /** Attempt the owner grant, then request the LAN permission before background enrollment. */
    private fun prepareLAN() {
        if (!Kylok.lanGranted(this)) {
            val dpm = getSystemService(DevicePolicyManager::class.java)
                ?: getSystemService(DEVICE_POLICY_SERVICE) as? DevicePolicyManager
            runCatching {
                dpm?.setPermissionGrantState(ComponentName(this, KylokDeviceAdminReceiver::class.java),
                    packageName, Kylok.LAN_PERMISSION,
                    DevicePolicyManager.PERMISSION_GRANT_STATE_GRANTED)
            }
            if (!Kylok.lanGranted(this)) {
                requestPermissions(arrayOf(Kylok.LAN_PERMISSION), 1)
                return
            }
        }
        complete()
    }

    /** Continue setup only after LAN access is granted, otherwise display the requirement. Usage: Android returns the runtime permission result. */
    override fun onRequestPermissionsResult(code: Int, permissions: Array<out String>, results: IntArray) {
        super.onRequestPermissionsResult(code, permissions, results)
        if (code == 1 && Kylok.lanGranted(this)) complete()
        else setContentView(TextView(this).apply {
            setText(R.string.kylok_lan_required)
        })
    }

    /** Schedule durable enrollment and signal successful compliance back to Setup Wizard. */
    private fun complete() {
        Kylok.schedule(this)
        setResult(RESULT_OK)
        finish()
    }
}
