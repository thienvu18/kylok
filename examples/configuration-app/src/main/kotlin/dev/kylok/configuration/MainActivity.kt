// SPDX-License-Identifier: LGPL-3.0-only
package dev.kylok.configuration

import android.app.Activity
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.RestrictionsManager
import android.os.Bundle
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView

/** Independent Android app: no Kylok dependency and no privileged DPC calls.
 * Applies only its own documented display_name/allow_refresh settings; arbitrary values are not logged.
 */
class MainActivity : Activity() {
    private lateinit var status: TextView
    private lateinit var refresh: Button
    private val changes = object : BroadcastReceiver() {
        /** Usage: Android invokes this while the visible app's restrictions change; re-read current values instead of trusting extras. */
        override fun onReceive(context: Context, intent: Intent) { applyConfiguration() }
    }
    /** Usage: Android creates this Activity; construct UI and read persisted restrictions even if a broadcast was missed. */
    override fun onCreate(state: Bundle?) {
        super.onCreate(state)
        status = TextView(this)
        refresh = Button(this).apply { text = getString(R.string.refresh); setOnClickListener { applyConfiguration() } }
        setContentView(LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; addView(status); addView(refresh) })
        applyConfiguration()
    }
    /** Usage: Android resumes the app; observe system-only change broadcasts while visible. */
    override fun onStart() {
        super.onStart()
        registerReceiver(changes, IntentFilter(Intent.ACTION_APPLICATION_RESTRICTIONS_CHANGED), Context.RECEIVER_NOT_EXPORTED)
        applyConfiguration()
    }
    /** Usage: Android stops the app; remove the receiver without altering managed settings. */
    override fun onStop() { unregisterReceiver(changes); super.onStop() }
    /** Apply app-owned behavior independently of the DPC's readback: update title and actual button availability. */
    private fun applyConfiguration() {
        val values = getSystemService(RestrictionsManager::class.java).applicationRestrictions
        val title = values.getString("display_name", getString(R.string.default_title))
        val enabled = values.getBoolean("allow_refresh", true)
        this.title = title
        refresh.isEnabled = enabled
        status.text = getString(R.string.status, title, values.keySet().size, enabled.toString())
    }
}
