// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.app.Activity
import android.app.admin.DevicePolicyManager
import android.content.Intent
import android.os.Bundle

class ProvisioningModeActivity : Activity() {
    /** Select Fully Managed mode only when Android offers it and provisioning extras are durable. Usage: Setup Wizard launches this merged provisioning Activity. */
    override fun onCreate(state: Bundle?) {
        super.onCreate(state)
        setResult(RESULT_CANCELED)
        if (intent.action == DevicePolicyManager.ACTION_GET_PROVISIONING_MODE) {
            val allowed = intent.getIntegerArrayListExtra(
                DevicePolicyManager.EXTRA_PROVISIONING_ALLOWED_PROVISIONING_MODES
            ).orEmpty()
            if (DevicePolicyManager.PROVISIONING_MODE_FULLY_MANAGED_DEVICE in allowed) {
                Kylok.capture(this, intent, "GET_PROVISIONING_MODE") { success ->
                    if (success) setResult(RESULT_OK, Intent().putExtra(DevicePolicyManager.EXTRA_PROVISIONING_MODE,
                        DevicePolicyManager.PROVISIONING_MODE_FULLY_MANAGED_DEVICE))
                    finish()
                }
                return
            }
        }
        finish()
    }
}
