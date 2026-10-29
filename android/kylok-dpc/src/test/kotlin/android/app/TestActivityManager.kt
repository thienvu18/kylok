// SPDX-License-Identifier: LGPL-3.0-only
package android.app

import android.content.pm.ConfigurationInfo

/** Test double for ActivityManager. */
class TestActivityManager : ActivityManager() {
    var modeState: Int = ActivityManager.LOCK_TASK_MODE_NONE
    var glesVersion: Int = 0x00020000

    override fun getLockTaskModeState(): Int = modeState
    override fun getDeviceConfigurationInfo(): ConfigurationInfo = ConfigurationInfo().apply {
        reqGlEsVersion = this@TestActivityManager.glesVersion
    }
}
