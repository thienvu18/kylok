// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.example

import android.Manifest
import android.app.Activity
import android.graphics.Color
import android.graphics.Typeface
import android.os.Build
import android.os.Bundle
import android.widget.*
import io.github.thienvu18.kylok.dpc.AgentStatus
import io.github.thienvu18.kylok.dpc.Kylok
import io.github.thienvu18.kylok.dpc.StatusListener
import java.io.Closeable
import java.text.DateFormat
import java.util.Date

/** Host-owned agent dashboard. Usage: Android launches it from the example launcher icon. */
class MainActivity : Activity() {
    // Match only this Activity's LAN prompt when Android delivers the permission result.
    private val lanPermissionRequest = 10
    private lateinit var content: LinearLayout
    private lateinit var header: TextView
    private var subscription: Closeable? = null
    private var current: AgentStatus? = null
    private var page = 0
    private val ink = Color.rgb(23, 61, 54)

    /** Build the host view without coupling provisioning or background work to Activity startup.
     * Usage: Android invokes this callback when opening or recreating the example.
     */
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        page = savedInstanceState?.getInt("page") ?: 0
        val layout = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; setPadding(dp(20), dp(20), dp(20), 0); setBackgroundColor(Color.rgb(245, 246, 239)) }
        layout.addView(text(getString(R.string.app_name), 32, true))
        header = text(getString(R.string.loading_status), 13); layout.addView(header)
        val tabs = LinearLayout(this)
        listOf(R.string.overview, R.string.applications, R.string.policies, R.string.operations, R.string.diagnostics).forEachIndexed { index, label ->
            tabs.addView(button(getString(label)) { page = index; render() })
        }
        layout.addView(HorizontalScrollView(this).apply { addView(tabs); isHorizontalScrollBarEnabled = false })
        content = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
        layout.addView(ScrollView(this).apply { addView(content) }, LinearLayout.LayoutParams(-1, 0, 1f))
        val actions = LinearLayout(this)
        actions.addView(button(getString(R.string.refresh)) { Kylok.retry(this); refresh() }, LinearLayout.LayoutParams(0, -2, 1f))
        actions.addView(button(getString(R.string.read_status)) { refresh() }, LinearLayout.LayoutParams(0, -2, 1f))
        layout.addView(actions); setContentView(layout); render()
    }

    /** Observe only while visible. Usage: Android invokes this when the Activity becomes visible. */
    override fun onStart() { super.onStart(); subscription = Kylok.observe(StatusListener { current = it; render() }); refresh() }

    /** Release host callbacks without stopping the agent. Usage: Android invokes this when the view is hidden. */
    override fun onStop() { subscription?.close(); subscription = null; super.onStop() }

    /** Preserve navigation only; protected agent data stays in the AAR. Usage: invoked by Android before recreation. */
    override fun onSaveInstanceState(outState: Bundle) { outState.putInt("page", page); super.onSaveInstanceState(outState) }

    /** Schedule a new attempt after a LAN grant. Usage: Android invokes the runtime permission callback. */
    override fun onRequestPermissionsResult(code: Int, permissions: Array<out String>, results: IntArray) {
        super.onRequestPermissionsResult(code, permissions, results)
        if (code == lanPermissionRequest) { Kylok.retry(this); refresh() }
    }

    /** Refresh local observations without pretending a requested network sync has completed. */
    private fun refresh() { Kylok.snapshot(this) { current = it; render() } }

    /** Scale spacing to display density instead of using fixed physical pixels. */
    private fun dp(value: Int) = (value * resources.displayMetrics.density).toInt()

    /** Make long identities and error reasons readable and selectable. */
    private fun text(value: String, size: Int = 14, bold: Boolean = false) = TextView(this).apply {
        text = value; textSize = size.toFloat(); setTextColor(ink); setPadding(0, dp(6), 0, dp(6)); setTextIsSelectable(true)
        if (bold) setTypeface(typeface, Typeface.BOLD)
    }

    /** Keep sync, permission and OS-confirmation actions behind explicit user presses. */
    private fun button(label: String, action: () -> Unit) = Button(this).apply { text = label; isAllCaps = false; setOnClickListener { action() } }

    /** Group related observations while keeping SDK implementation objects outside the host view. */
    private fun card(title: String, body: String) {
        val group = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; setPadding(dp(16), dp(10), dp(16), dp(10)); setBackgroundColor(Color.WHITE) }
        group.addView(text(title, 17, true)); group.addView(text(body))
        content.addView(group, LinearLayout.LayoutParams(-1, -2).apply { bottomMargin = dp(12) })
    }

    /** Keep desired, observed, pending and failed states distinct in every section. */
    private fun render() {
        content.removeAllViews()
        val status = current ?: run { content.addView(text(getString(R.string.loading_status))); return }
        header.text = getString(R.string.agent_heading, status.lifecycle, status.sdkVersion)
        when (page) {
            0 -> {
                card(getString(R.string.management), getString(R.string.management_values, status.deviceOwner.toString(), status.scopeId ?: "—", status.deviceId ?: "—"))
                card(getString(R.string.connection), getString(R.string.connection_values, status.serverOrigin ?: "—", status.lanPermissionGranted.toString(),
                    if (status.lastSyncUnixMillis > 0) DateFormat.getDateTimeInstance().format(Date(status.lastSyncUnixMillis)) else getString(R.string.not_synced), status.desiredRevision, status.syncSequence, status.pendingReports))
                card(getString(R.string.last_result), status.lastResult)
                if (!status.deviceOwner) card(getString(R.string.provisioning_title), getString(R.string.provisioning_help))
                if (Build.VERSION.SDK_INT >= 37 && !status.lanPermissionGranted) content.addView(button(getString(R.string.allow_lan)) {
                    requestPermissions(arrayOf(Manifest.permission.ACCESS_LOCAL_NETWORK), lanPermissionRequest)
                })
                if (status.operations.any { it.requiresUserAction }) content.addView(button(getString(R.string.confirm_install)) { Kylok.confirmPendingInstall(this) })
            }
            1 -> {
                content.addView(text(getString(R.string.apps_help)))
                if (status.applications.isEmpty()) card(getString(R.string.applications), getString(R.string.no_apps))
                status.applications.forEach { app -> card(app.packageName, getString(R.string.app_values, app.desiredVersion, app.observedVersion?.toString() ?: "—", app.state, app.reason)) }
            }
            2 -> {
                content.addView(text(getString(R.string.policies_help)))
                if (status.policies.isEmpty()) card(getString(R.string.policies), getString(R.string.no_policies))
                status.policies.forEach { policy -> card(policy.ruleId, getString(R.string.policy_values, policy.state, policy.reason.ifBlank { "—" }, policy.observedValue.ifBlank { "—" })) }
            }
            3 -> {
                content.addView(text(getString(R.string.operations_help)))
                if (status.operations.isEmpty()) card(getString(R.string.operations), getString(R.string.no_operations))
                status.operations.forEach { operation ->
                    card(operation.packageName, getString(R.string.operation_values, operation.stage, operation.version, operation.downloadedBytes, operation.sizeBytes, operation.error.ifBlank { "—" }, operation.id))
                    content.addView(ProgressBar(this, null, android.R.attr.progressBarStyleHorizontal).apply {
                        // Convert bounded artifact bytes to a percentage only for display; SDK checkpoints retain exact bytes.
                        max = 100; progress = if (operation.sizeBytes > 0) (100 * operation.downloadedBytes / operation.sizeBytes).coerceIn(0, 100).toInt() else 0
                    })
                    if (operation.requiresUserAction) content.addView(button(getString(R.string.confirm_install)) { Kylok.confirmPendingInstall(this) })
                }
            }
            else -> {
                card(getString(R.string.device), "${Build.MANUFACTURER} ${Build.MODEL}\nAPI ${Build.VERSION.SDK_INT}\n${Build.FINGERPRINT}")
                card(getString(R.string.sdk_identity), getString(R.string.sdk_values, status.sdkVersion, status.protocolVersion, packageName))
                card(getString(R.string.observation_limits), getString(R.string.limits_help))
            }
        }
    }
}
