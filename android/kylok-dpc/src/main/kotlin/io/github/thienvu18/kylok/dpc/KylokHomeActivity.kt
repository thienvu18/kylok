// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.app.Activity
import android.app.ActivityManager
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.Gravity
import android.widget.LinearLayout
import android.widget.TextView
import android.window.OnBackInvokedCallback
import android.window.OnBackInvokedDispatcher

/** Persistent HOME activity and foreground app recovery for managed kiosk deployments.
 * Android routes the HOME intent to this activity when configured as persistent preferred activity.
 * Usage: Android launches this activity at device boot, on Home press, or after foreground process loss.
 */
class KylokHomeActivity : Activity() {
    private val handler = Handler(Looper.getMainLooper())
    private var statusView: TextView? = null

    /** Predictive back callback suppressing back gestures to maintain persistent launcher visibility. */
    private val backCallback = OnBackInvokedCallback {
        // Suppress back gesture on persistent launcher
    }

    /** Receives dynamic package replacement broadcasts to relaunch updated kiosk applications. */
    private val packageReceiver = object : BroadcastReceiver() {
        /** Receive package update and replacement broadcasts. Usage: Android invokes this callback when a package is updated. */
        override fun onReceive(context: Context, intent: Intent) {
            handler.post { reconcileAndLaunch() }
        }
    }

    /** Receives keyguard unlock broadcasts to resume kiosk launch once the screen is unlocked. */
    private val userPresentReceiver = object : BroadcastReceiver() {
        /** Receive keyguard unlock broadcasts. Usage: Android invokes this callback when the user unlocks the screen. */
        override fun onReceive(context: Context, intent: Intent) {
            handler.post { reconcileAndLaunch() }
        }
    }

    /** Initialize views, register package and keyguard receivers, and trigger recovery. Usage: Android invokes this Activity lifecycle callback when HOME is requested. */
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        Kylok.initialize()
        val layout = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER
            setPadding(64, 64, 64, 64)
        }
        statusView = TextView(this).apply {
            gravity = Gravity.CENTER
            textSize = 16f
            setText(R.string.kylok_launcher_starting)
        }
        layout.addView(statusView)
        setContentView(layout)

        runCatching {
            onBackInvokedDispatcher.registerOnBackInvokedCallback(
                OnBackInvokedDispatcher.PRIORITY_DEFAULT,
                backCallback
            )
        }

        // Protected system broadcasts originate from the OS (system_server UID);
        // RECEIVER_EXPORTED ensures delivery across process boundaries on Android 13+.
        val packageFilter = IntentFilter().apply {
            addAction(Intent.ACTION_PACKAGE_ADDED)
            addAction(Intent.ACTION_PACKAGE_REPLACED)
            addDataScheme("package")
        }
        registerReceiver(packageReceiver, packageFilter, Context.RECEIVER_EXPORTED)

        val userPresentFilter = IntentFilter(Intent.ACTION_USER_PRESENT)
        registerReceiver(userPresentReceiver, userPresentFilter, Context.RECEIVER_EXPORTED)
    }

    @Suppress("DEPRECATION", "OVERRIDE_DEPRECATION")
    /** Suppress back key events to retain the persistent HOME launcher screen. Usage: Android invokes this Activity callback when a back key event occurs. */
    override fun onBackPressed() {
        // Retain launcher screen as the persistent HOME activity.
    }

    /** Reconcile running kiosk state and recover the foreground application if needed. Usage: Android invokes this Activity callback when gaining foreground focus. */
    override fun onResume() {
        super.onResume()
        if (intent?.getBooleanExtra(EXTRA_EXIT, false) == true) {
            exitLockTaskAndFinish()
            return
        }
        reconcileAndLaunch()
    }

    /** Handle new intent deliveries such as explicit launcher exit requests. Usage: Android delivers re-launched intents to this singleInstance Activity. */
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        if (intent.getBooleanExtra(EXTRA_EXIT, false)) {
            exitLockTaskAndFinish()
        }
    }

    /** Exit Lock Task mode if currently active on this Activity and finish. */
    private fun exitLockTaskAndFinish() {
        val am = getSystemService(ActivityManager::class.java)
            ?: getSystemService(ACTIVITY_SERVICE) as? ActivityManager
        if (am?.lockTaskModeState == ActivityManager.LOCK_TASK_MODE_LOCKED) {
            runCatching { stopLockTask() }
        }
        finish()
    }

    /** Unregister dynamic broadcast receivers, predictive back callback, and clear pending recovery callbacks. Usage: Android invokes this Activity callback when destroyed. */
    override fun onDestroy() {
        handler.removeCallbacksAndMessages(null)
        runCatching { onBackInvokedDispatcher.unregisterOnBackInvokedCallback(backCallback) }
        runCatching { unregisterReceiver(packageReceiver) }
        runCatching { unregisterReceiver(userPresentReceiver) }
        super.onDestroy()
    }

    /** Query persistent policy, verify prerequisites, and launch or throttle the target application. */
    private fun reconcileAndLaunch() {
        handler.removeCallbacksAndMessages(null)
        val state = runCatching { AgentStorage.read(this) }.getOrNull()
        if (state == null) {
            finish()
            return
        }
        val platform = AndroidLauncherPlatform(this, AgentStorage.access(this))
        val result = LauncherRecovery.reconcile(state, platform)
        when (result.outcome) {
            RecoveryOutcome.NOT_OWNER, RecoveryOutcome.CLEARED -> finish()
            RecoveryOutcome.PREREQUISITES_PENDING -> statusView?.setText(R.string.kylok_launcher_waiting)
            RecoveryOutcome.DEVICE_LOCKED -> statusView?.setText(R.string.kylok_launcher_locked)
            RecoveryOutcome.ALLOWLIST_PENDING, RecoveryOutcome.NOT_LAUNCHABLE ->
                statusView?.text = getString(R.string.kylok_launcher_starting)
            RecoveryOutcome.ALREADY_RUNNING -> Unit
            RecoveryOutcome.LAUNCHED -> statusView?.setText(R.string.kylok_launcher_starting)
            RecoveryOutcome.THROTTLED -> {
                val delaySeconds = (result.throttleDelayMs + 999) / 1000
                statusView?.text = getString(R.string.kylok_launcher_recovering) + " (${delaySeconds}s)"
                handler.postDelayed({ reconcileAndLaunch() }, result.throttleDelayMs)
            }
        }
    }

    companion object {
        /** Intent extra signaling an explicit request to exit the persistent HOME launcher. */
        const val EXTRA_EXIT = "io.github.thienvu18.kylok.dpc.extra.EXIT"
    }
}
