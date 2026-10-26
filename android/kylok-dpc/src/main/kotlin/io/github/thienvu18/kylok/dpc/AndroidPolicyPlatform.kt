// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.app.ActivityManager
import android.app.ActivityOptions
import android.app.KeyguardManager
import android.app.admin.DevicePolicyManager
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.PermissionInfo
import android.os.Build
import android.os.UserManager
import android.provider.Settings
import io.github.thienvu18.kylok.proto.v1.InstalledApplication
import org.json.JSONObject

internal class AndroidPolicyPlatform(private val context: Context, private val storage: StateAccess) : PolicyPlatform {
    private val dpm = context.getSystemService(DevicePolicyManager::class.java)
    private val admin = ComponentName(context, KylokDeviceAdminReceiver::class.java)
    private val packages = context.packageManager
    private val installer = AndroidInstallPlatform(context, storage)
    private val activityManager = context.getSystemService(ActivityManager::class.java)
    /** Read wall time for policy attempt throttling. */
    override fun now() = System.currentTimeMillis()
    /** Read effective ownership before every privileged setter. */
    override fun isOwner() = dpm.isDeviceOwnerApp(context.packageName)
    /** Reuse verified package observations for required-app prerequisites. */
    override fun installed(packageName: String): InstalledApplication? = installer.installed(packageName)

    /** Reject unsupported restrictions, permission states or unavailable runtime grants before mutation. */
    override fun check(setting: PolicySetting) {
        if (!isOwner()) throw SecurityException("Device Owner required")
        when (setting.kind) {
            PolicyKind.RESTRICTION -> if (setting.name !in RESTRICTIONS) throw PolicyUnsupported("RESTRICTION_UNSUPPORTED")
            PolicyKind.PERMISSION -> {
                val app = try { packages.getPackageInfo(setting.target, PackageManager.GET_PERMISSIONS) }
                    catch (_: PackageManager.NameNotFoundException) { throw PolicyPending("REQUIRED_APP_PENDING") }
                val info = try { packages.getPermissionInfo(setting.name, 0) }
                    catch (_: PackageManager.NameNotFoundException) { throw PolicyUnsupported("PERMISSION_UNKNOWN") }
                if (app.applicationInfo!!.targetSdkVersion < 23 || setting.name !in (app.requestedPermissions?.toList() ?: emptyList()) ||
                    info.protection != PermissionInfo.PROTECTION_DANGEROUS)
                    throw PolicyUnsupported("PERMISSION_NOT_RUNTIME_OR_REQUESTED")
                if (setting.value == "GRANTED" && (setting.name in SENSOR_PERMISSIONS ||
                    Build.VERSION.SDK_INT >= 36 && setting.name.startsWith("android.permission.health.")) && !dpm.canAdminGrantSensorsPermissions())
                    throw PolicyUnsupported("SENSOR_GRANT_UNSUPPORTED")
                if (setting.value !in setOf("DEFAULT", "GRANTED", "DENIED")) throw PolicyUnsupported("PERMISSION_STATE_UNSUPPORTED")
            }
            else -> Unit
        }
    }
    /** Translate the administrator permission-grant enum into the shared policy representation. */
    private fun permissionState(setting: PolicySetting): String = when (dpm.getPermissionGrantState(admin, setting.target, setting.name)) {
        DevicePolicyManager.PERMISSION_GRANT_STATE_DEFAULT -> "DEFAULT"
        DevicePolicyManager.PERMISSION_GRANT_STATE_GRANTED -> "GRANTED"
        DevicePolicyManager.PERMISSION_GRANT_STATE_DENIED -> "DENIED"
        else -> "unknown"
    }
    /** Read this administrator’s local/global contribution for baseline restoration. */
    private fun ownRestriction(name: String): Boolean = dpm.getUserRestrictions(admin).getBoolean(name) ||
        (Build.VERSION.SDK_INT >= 34 && dpm.userRestrictionsGlobally.getBoolean(name))
    /** Canonicalize the current administrator Lock Task allowlist. */
    private fun lockPackages() = dpm.getLockTaskPackages(admin).toList().sorted()
    /** Require actual managed Lock Task plus a callback recorded during the current boot. */
    private fun lockedPackage(): String {
        if (activityManager.lockTaskModeState != ActivityManager.LOCK_TASK_MODE_LOCKED) return ""
        val state = storage.read()
        val boot = bootCount(context)
        return if (boot >= 0 && state.optInt("lock_task_boot", -1) == boot) state.optString("lock_task_package", "unknown") else "unknown"
    }
    /** Capture this administrator’s baseline without replaying an unrelated earlier activity launch. */
    override fun capture(setting: PolicySetting): String = when (setting.kind) {
        PolicyKind.CAMERA -> dpm.getCameraDisabled(admin).toString()
        PolicyKind.SCREEN_CAPTURE -> dpm.getScreenCaptureDisabled(admin).toString()
        PolicyKind.MAXIMUM_TIME_TO_LOCK -> dpm.getMaximumTimeToLock(admin).toString()
        PolicyKind.RESTRICTION -> ownRestriction(setting.name).toString()
        PolicyKind.PERMISSION -> permissionState(setting)
        // Preserve the host's allowlist. An earlier external activity launch is
        // not replayed as a side effect of releasing Kylok's kiosk policy.
        PolicyKind.LOCK_TASK -> lockTaskValue(lockPackages(), "")
    }
    /** Read effective policy state and verify permission grants and managed Lock Task observations. */
    override fun observe(setting: PolicySetting): String = when (setting.kind) {
        PolicyKind.CAMERA -> dpm.getCameraDisabled(null).toString()
        PolicyKind.SCREEN_CAPTURE -> dpm.getScreenCaptureDisabled(null).toString()
        PolicyKind.MAXIMUM_TIME_TO_LOCK -> dpm.getMaximumTimeToLock(null).toString()
        PolicyKind.RESTRICTION -> context.getSystemService(UserManager::class.java).hasUserRestriction(setting.name).toString()
        PolicyKind.PERMISSION -> {
            val state = permissionState(setting)
            val granted = packages.checkPermission(setting.name, setting.target) == PackageManager.PERMISSION_GRANTED
            if (state == "GRANTED" && !granted || state == "DENIED" && granted) "${state}_NOT_EFFECTIVE" else state
        }
        PolicyKind.LOCK_TASK -> lockTaskValue(lockPackages(), lockedPackage())
    }
    /** Recheck prerequisites and apply the journaled restriction or safe kiosk transition. */
    override fun apply(setting: PolicySetting) {
        // Recheck ownership immediately before every privileged effect.
        check(setting)
        when (setting.kind) {
            PolicyKind.CAMERA -> dpm.setCameraDisabled(admin, setting.value.toBooleanStrict())
            PolicyKind.SCREEN_CAPTURE -> dpm.setScreenCaptureDisabled(admin, setting.value.toBooleanStrict())
            PolicyKind.MAXIMUM_TIME_TO_LOCK -> dpm.setMaximumTimeToLock(admin, setting.value.toLong())
            PolicyKind.RESTRICTION -> if (setting.value.toBooleanStrict()) dpm.addUserRestriction(admin, setting.name) else dpm.clearUserRestriction(admin, setting.name)
            PolicyKind.PERMISSION -> {
                val state = when (setting.value) {
                    "GRANTED" -> DevicePolicyManager.PERMISSION_GRANT_STATE_GRANTED
                    "DENIED" -> DevicePolicyManager.PERMISSION_GRANT_STATE_DENIED
                    else -> DevicePolicyManager.PERMISSION_GRANT_STATE_DEFAULT
                }
                if (!dpm.setPermissionGrantState(admin, setting.target, setting.name, state)) throw PolicyUnsupported("PERMISSION_CHANGE_REJECTED")
            }
            PolicyKind.LOCK_TASK -> {
                val value = JSONObject(setting.value)
                val names = value.getJSONArray("packages").let { list -> (0 until list.length()).map { list.getString(it) } }
                val kiosk = value.getString("kiosk_package")
                // Clearing first also exits an old locked task, including when
                // its package remains in the new allowlist. Screen pinning is
                // never accepted as managed Lock Task mode.
                dpm.setLockTaskPackages(admin, emptyArray())
                dpm.setLockTaskPackages(admin, names.toTypedArray())
                if (kiosk.isNotEmpty()) {
                    if (context.getSystemService(KeyguardManager::class.java).isDeviceLocked) throw PolicyPending("DEVICE_LOCKED")
                    if (!dpm.isLockTaskPermitted(kiosk)) throw PolicyPending("ALLOWLIST_PENDING")
                    val launch = packages.getLaunchIntentForPackage(kiosk) ?: throw PolicyUnsupported("KIOSK_NOT_LAUNCHABLE")
                    val options = ActivityOptions.makeBasic().setLockTaskEnabled(true)
                    context.startActivity(launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK), options.toBundle())
                }
            }
        }
    }
    companion object {
        val CAPABILITIES = listOf("policy.camera", "policy.screen_capture", "policy.maximum_time_to_lock", "policy.restrictions", "policy.permissions", "policy.kiosk")
        val RESTRICTIONS = setOf(UserManager.DISALLOW_ADD_USER, UserManager.DISALLOW_DEBUGGING_FEATURES,
            UserManager.DISALLOW_MODIFY_ACCOUNTS, UserManager.DISALLOW_UNINSTALL_APPS)
        private val SENSOR_PERMISSIONS = setOf("android.permission.ACCESS_FINE_LOCATION", "android.permission.ACCESS_COARSE_LOCATION",
            "android.permission.ACCESS_BACKGROUND_LOCATION", "android.permission.CAMERA", "android.permission.RECORD_AUDIO",
            "android.permission.RECORD_BACKGROUND_AUDIO", "android.permission.ACTIVITY_RECOGNITION",
            "android.permission.BODY_SENSORS", "android.permission.BODY_SENSORS_BACKGROUND")
        /** Read the boot generation, treating unavailable values as unknown rather than confirming kiosk entry. */
        fun bootCount(context: Context) = Settings.Global.getInt(context.contentResolver, Settings.Global.BOOT_COUNT, -1)
    }
}
