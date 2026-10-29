// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.app.Activity
import android.app.ActivityManager
import android.app.ActivityOptions
import android.app.KeyguardManager
import android.app.admin.DevicePolicyManager
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import io.github.thienvu18.kylok.proto.v1.InstalledApplication

/** Android platform adapter for persistent HOME registration, Lock Task transitions, and application launches. */
internal class AndroidLauncherPlatform(
    private val context: Context,
    private val storage: StateAccess
) : LauncherPlatform {
    private val dpm = context.getSystemService(DevicePolicyManager::class.java)
        ?: context.getSystemService(Context.DEVICE_POLICY_SERVICE) as? DevicePolicyManager ?: error("DPM required")
    private val admin = ComponentName(context, KylokDeviceAdminReceiver::class.java)
    private val packages = context.packageManager
    private val activityManager = context.getSystemService(ActivityManager::class.java)
        ?: context.getSystemService(Context.ACTIVITY_SERVICE) as? ActivityManager
    private val keyguard = context.getSystemService(KeyguardManager::class.java)
        ?: context.getSystemService(Context.KEYGUARD_SERVICE) as? KeyguardManager
    private val installer = AndroidInstallPlatform(context, storage)

    /** Read current wall clock time in milliseconds. */
    override fun now() = System.currentTimeMillis()

    /** Check whether this application is registered as Device Owner. */
    override fun isOwner() = dpm.isDeviceOwnerApp(context.packageName)

    /** Check whether the device is currently keyguard-locked. */
    override fun isDeviceLocked(): Boolean = keyguard?.isDeviceLocked == true

    /** Query Lock Task allowlist permission for the package name. */
    override fun isLockTaskPermitted(packageName: String) = dpm.isLockTaskPermitted(packageName)

    /** Check whether the device is currently running in managed Lock Task mode. */
    override fun isLockTaskLocked() = activityManager?.lockTaskModeState == ActivityManager.LOCK_TASK_MODE_LOCKED

    /** Check whether persistent HOME resolves to Kylok's HOME activity. */
    override fun isHomePreferred(): Boolean {
        val intent = Intent(Intent.ACTION_MAIN)
        intent.addCategory(Intent.CATEGORY_HOME)
        val resolve = packages.resolveActivity(intent, PackageManager.MATCH_DEFAULT_ONLY)
            ?: packages.resolveActivity(intent, 0)
        return resolve?.activityInfo?.packageName == context.packageName &&
            resolve?.activityInfo?.name == KylokHomeActivity::class.java.name
    }

    /** Register or clear persistent preferred activity for HOME. */
    override fun setHomePreferred(preferred: Boolean) {
        val home = ComponentName(context, KylokHomeActivity::class.java)
        if (preferred) {
            packages.setComponentEnabledSetting(
                home,
                PackageManager.COMPONENT_ENABLED_STATE_ENABLED,
                PackageManager.DONT_KILL_APP
            )
            val filter = IntentFilter(Intent.ACTION_MAIN).apply {
                addCategory(Intent.CATEGORY_HOME)
                addCategory(Intent.CATEGORY_DEFAULT)
            }
            dpm.addPersistentPreferredActivity(admin, filter, home)
        } else {
            dpm.clearPackagePersistentPreferredActivities(admin, context.packageName)
            packages.setComponentEnabledSetting(
                home,
                PackageManager.COMPONENT_ENABLED_STATE_DISABLED,
                PackageManager.DONT_KILL_APP
            )
        }
    }

    /** Inspect installed application metadata and signing certificates. */
    override fun installedApp(packageName: String): InstalledApplication? = installer.installed(packageName)

    /** Resolve launchable Intent from package launcher or home category. */
    override fun isAppLaunchable(packageName: String): Boolean {
        return packages.getLaunchIntentForPackage(packageName) != null ||
            packages.queryIntentActivities(
                Intent(Intent.ACTION_MAIN).apply {
                    setPackage(packageName)
                    addCategory(Intent.CATEGORY_HOME)
                }, 0
            ).isNotEmpty()
    }

    /** Start the target application in managed Lock Task mode. */
    override fun launchAppInLockTask(packageName: String) {
        val launch = packages.getLaunchIntentForPackage(packageName)
            ?: packages.queryIntentActivities(
                Intent(Intent.ACTION_MAIN).apply {
                    setPackage(packageName)
                    addCategory(Intent.CATEGORY_HOME)
                }, 0
            ).firstOrNull()?.let {
                Intent(Intent.ACTION_MAIN).apply {
                    addCategory(Intent.CATEGORY_HOME)
                    component = ComponentName(it.activityInfo.packageName, it.activityInfo.name)
                }
            } ?: return
        val options = ActivityOptions.makeBasic()?.setLockTaskEnabled(true)
        val target = Intent(launch).apply { addFlags(Intent.FLAG_ACTIVITY_NEW_TASK) }
        context.startActivity(target, options?.toBundle())
    }

    /** Finish the launcher activity if running in an Activity context, or send an exit intent. */
    override fun finishLauncher() {
        if (context is Activity) {
            context.finish()
        } else {
            val exitIntent = Intent(context, KylokHomeActivity::class.java).apply {
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                putExtra(KylokHomeActivity.EXTRA_EXIT, true)
            }
            context.startActivity(exitIntent)
        }
    }

    /** Persist crash count and recovery backoff state across activity and process lifecycles. */
    override fun updateRecovery(kioskPackage: String, crashCount: Int, throttledUntil: Long, lastLaunchAt: Long) {
        runCatching {
            storage.update {
                it.objectValue("launcher_recovery")
                    .put("kiosk_package", kioskPackage)
                    .put("crash_count", crashCount)
                    .put("throttled_until", throttledUntil)
                    .put("last_launch_at", lastLaunchAt)
            }
        }
    }
}
