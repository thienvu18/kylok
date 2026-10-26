// SPDX-License-Identifier: LGPL-3.0-only
package dev.kylok.consumer

import android.app.Activity
import android.os.Bundle
import android.widget.TextView
import io.github.thienvu18.kylok.dpc.Kylok
import io.github.thienvu18.kylok.dpc.AgentStatus
import io.github.thienvu18.kylok.dpc.StatusListener
import java.io.Closeable

/** A new host imports the published facade; no SDK sources or internal APIs. */
class MainActivity : Activity() {
    private var subscription: Closeable? = null
    override fun onCreate(state: Bundle?) {
        super.onCreate(state)
        val view = TextView(this)
        fun show(status: AgentStatus) { view.text = getString(R.string.status, Kylok.sdkVersion, status.lifecycle) }
        subscription = Kylok.observe(StatusListener(::show))
        Kylok.snapshot(this, ::show)
        setContentView(view)
    }
    override fun onDestroy() { subscription?.close(); super.onDestroy() }
}
