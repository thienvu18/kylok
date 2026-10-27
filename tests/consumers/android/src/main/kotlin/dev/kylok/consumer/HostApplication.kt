// SPDX-License-Identifier: LGPL-3.0-only
package dev.kylok.consumer

import android.app.Application
import io.github.thienvu18.kylok.dpc.HostOptions
import io.github.thienvu18.kylok.dpc.Kylok

/** The host configures the packaged SDK before component/worker work, without requiring an Activity. */
class HostApplication : Application() {
    /** Android invokes this on every process creation, including worker/receiver starts. */
    override fun onCreate() { super.onCreate(); Kylok.configure(this, HostOptions(syncIntervalMinutes = 30)) }
}
