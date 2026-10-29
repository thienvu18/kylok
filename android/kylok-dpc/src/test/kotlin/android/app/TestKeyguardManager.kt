// SPDX-License-Identifier: LGPL-3.0-only
package android.app

/** Test double for KeyguardManager. */
class TestKeyguardManager : KeyguardManager() {
    var deviceLocked: Boolean = false
    override fun isDeviceLocked(): Boolean = deviceLocked
}
