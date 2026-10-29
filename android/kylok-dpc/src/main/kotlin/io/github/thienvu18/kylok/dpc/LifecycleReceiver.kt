// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

class LifecycleReceiver : BroadcastReceiver() {
    /** Recover durable work after boot or host-package replacement. Usage: Android invokes this manifest receiver for the registered lifecycle broadcasts. */
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action == Intent.ACTION_BOOT_COMPLETED || intent.action == Intent.ACTION_MY_PACKAGE_REPLACED) {
            Kylok.schedule(context)
            LauncherRecovery.recover(context)
        }
    }
}
