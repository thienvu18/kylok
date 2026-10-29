// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.app.ActivityManager
import android.app.ActivityOptions
import android.app.KeyguardManager
import android.app.admin.DevicePolicyManager
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.ApplicationInfo
import android.content.pm.PackageManager
import android.content.pm.PermissionInfo
import android.os.Build
import android.os.UserManager
import android.provider.Settings
import io.github.thienvu18.kylok.proto.v1.InstalledApplication
import io.github.thienvu18.kylok.proto.v1.SyncResponse
import org.json.JSONObject

internal class AndroidPolicyPlatform(private val context: Context, private val storage: StateAccess) : PolicyPlatform {
    private val dpm = (context.getSystemService(DevicePolicyManager::class.java) ?: context.getSystemService(Context.DEVICE_POLICY_SERVICE)) as DevicePolicyManager
    private val admin = ComponentName(context, KylokDeviceAdminReceiver::class.java)
    private val packages = context.packageManager
    private val installer = AndroidInstallPlatform(context, storage)
    private val activityManager = (context.getSystemService(ActivityManager::class.java) ?: context.getSystemService(Context.ACTIVITY_SERVICE)) as ActivityManager
    private val keyguard = context.getSystemService(KeyguardManager::class.java) ?: context.getSystemService(Context.KEYGUARD_SERVICE) as? KeyguardManager
    private val userManager = context.getSystemService(UserManager::class.java) ?: context.getSystemService(Context.USER_SERVICE) as? UserManager
    /** Read wall time for policy attempt throttling. */
    override fun now() = System.currentTimeMillis()
    /** Read effective ownership before every privileged setter. */
    override fun isOwner() = dpm.isDeviceOwnerApp(context.packageName)
    /** Reuse verified package observations for required-app prerequisites. */
    override fun installed(packageName: String): InstalledApplication? = installer.installed(packageName)

    /** Return all installed user-uninstallable package names on this user profile. */
    private fun userUninstallablePackages(): List<String> = try {
        packages.getInstalledPackages(0).filter { pkg ->
            val info = pkg.applicationInfo
            val isPureSystem = info != null &&
                ((info.flags and ApplicationInfo.FLAG_SYSTEM) != 0) &&
                ((info.flags and ApplicationInfo.FLAG_UPDATED_SYSTEM_APP) == 0)
            !isPureSystem
        }.map { it.packageName }
    } catch (_: Exception) {
        emptyList()
    }

    /** Return required packages from the latest persisted snapshot policy document. */
    private fun requiredPackages(): Set<String> = try {
        val state = storage.read()
        if (!state.has("snapshot")) emptySet() else {
            val snapshot = SyncResponse.parseFrom(decode(state.getString("snapshot")))
            if (!snapshot.hasDesiredPolicy()) emptySet() else {
                snapshot.desiredPolicy.policy.requiredPackagesList.toSet()
            }
        }
    } catch (_: Exception) {
        emptySet()
    }

    /** Set whether package uninstallation is blocked at the OS PackageManager level. */
    override fun setUninstallBlocked(packageName: String, blocked: Boolean) {
        if (!isOwner()) return
        try {
            dpm.setUninstallBlocked(admin, packageName, blocked)
        } catch (_: Exception) {
            // Ignored if package is missing or cannot be modified.
        }
    }

    /** Observe whether package uninstallation is currently blocked at the OS PackageManager level. */
    override fun isUninstallBlocked(packageName: String): Boolean {
        if (!isOwner()) return false
        return try {
            dpm.isUninstallBlocked(admin, packageName)
        } catch (_: Exception) {
            false
        }
    }

    /** Reject unsupported restrictions, permission states or unavailable runtime grants before mutation. */
    override fun check(setting: PolicySetting) {
        if (!isOwner()) throw SecurityException("Device Owner required")
        when (setting.kind) {
            PolicyKind.RESTRICTION -> if (setting.name !in RESTRICTIONS) throw PolicyUnsupported("RESTRICTION_UNSUPPORTED")
            PolicyKind.MANAGED_CONFIGURATION -> {
                if (installed(setting.target) == null) throw PolicyPending("REQUIRED_APP_PENDING")
                ConfigurationValues.read(setting.value)
            }
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
    /** Check whether persistent HOME resolves to Kylok's HOME activity. */
    private fun isHomePreferred(): Boolean {
        val intent = Intent(Intent.ACTION_MAIN)
        intent.addCategory(Intent.CATEGORY_HOME)
        val resolve = packages.resolveActivity(intent, PackageManager.MATCH_DEFAULT_ONLY)
            ?: packages.resolveActivity(intent, 0)
        return resolve?.activityInfo?.packageName == context.packageName &&
            resolve?.activityInfo?.name == KylokHomeActivity::class.java.name
    }
    /** Require actual managed Lock Task, current boot callback, and persistent HOME configuration. */
    private fun lockedPackage(): String {
        val home = isHomePreferred()
        if (activityManager.lockTaskModeState != ActivityManager.LOCK_TASK_MODE_LOCKED) {
            return if (home) "LAUNCHER_PREFERRED_NOT_LOCKED" else ""
        }
        if (!home) return "LOCKED_WITHOUT_LAUNCHER"
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
        PolicyKind.MANAGED_CONFIGURATION -> ConfigurationValues.journal(AndroidConfigurations.capture(dpm.getApplicationRestrictions(admin, setting.target)))
        // Preserve the host's allowlist. An earlier external activity launch is
        // not replayed as a side effect of releasing Kylok's kiosk policy.
        PolicyKind.LOCK_TASK -> lockTaskValue(lockPackages(), "")
    }
    /** Read effective policy state and verify permission grants and managed Lock Task observations. */
    override fun observe(setting: PolicySetting): String = when (setting.kind) {
        PolicyKind.CAMERA -> dpm.getCameraDisabled(null).toString()
        PolicyKind.SCREEN_CAPTURE -> dpm.getScreenCaptureDisabled(null).toString()
        PolicyKind.MAXIMUM_TIME_TO_LOCK -> dpm.getMaximumTimeToLock(null).toString()
        PolicyKind.RESTRICTION -> {
            val userRestricted = userManager?.hasUserRestriction(setting.name) == true
            if (setting.name == UserManager.DISALLOW_UNINSTALL_APPS) {
                if (!userRestricted) "false"
                else {
                    val targets = userUninstallablePackages()
                    val allBlocked = targets.all { isUninstallBlocked(it) }
                    if (allBlocked) "true" else "UNINSTALL_BLOCKED_PENDING"
                }
            } else {
                userRestricted.toString()
            }
        }
        PolicyKind.MANAGED_CONFIGURATION -> capture(setting)
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
            PolicyKind.RESTRICTION -> {
                val enabled = setting.value.toBooleanStrict()
                if (enabled) dpm.addUserRestriction(admin, setting.name) else dpm.clearUserRestriction(admin, setting.name)
                if (setting.name == UserManager.DISALLOW_UNINSTALL_APPS) {
                    val targets = userUninstallablePackages()
                    val required = if (enabled) emptySet() else requiredPackages()
                    for (pkg in targets) {
                        if (enabled || pkg !in required) {
                            setUninstallBlocked(pkg, enabled)
                        }
                    }
                }
            }
            PolicyKind.MANAGED_CONFIGURATION -> dpm.setApplicationRestrictions(admin, setting.target,
                AndroidConfigurations.bundle(ConfigurationValues.read(setting.value)))
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
                val homeFilter = IntentFilter(Intent.ACTION_MAIN).apply {
                    addCategory(Intent.CATEGORY_HOME)
                    addCategory(Intent.CATEGORY_DEFAULT)
                }
                val homeComponent = ComponentName(context, KylokHomeActivity::class.java)
                if (kiosk.isNotEmpty()) {
                    packages.setComponentEnabledSetting(
                        homeComponent,
                        PackageManager.COMPONENT_ENABLED_STATE_ENABLED,
                        PackageManager.DONT_KILL_APP
                    )
                    dpm.addPersistentPreferredActivity(admin, homeFilter, homeComponent)
                    if (keyguard?.isDeviceLocked == true) throw PolicyPending("DEVICE_LOCKED")
                    if (!dpm.isLockTaskPermitted(kiosk)) throw PolicyPending("ALLOWLIST_PENDING")
                    val launch = packages.getLaunchIntentForPackage(kiosk)
                        ?: packages.queryIntentActivities(
                            Intent(Intent.ACTION_MAIN).apply {
                                setPackage(kiosk)
                                addCategory(Intent.CATEGORY_HOME)
                            }, 0
                        ).firstOrNull()?.let {
                            Intent(Intent.ACTION_MAIN).apply {
                                addCategory(Intent.CATEGORY_HOME)
                                component = ComponentName(it.activityInfo.packageName, it.activityInfo.name)
                            }
                        } ?: throw PolicyUnsupported("KIOSK_NOT_LAUNCHABLE")
                    val options = ActivityOptions.makeBasic()?.setLockTaskEnabled(true)
                    val target = Intent(launch).apply { addFlags(Intent.FLAG_ACTIVITY_NEW_TASK) }
                    context.startActivity(target, options?.toBundle())
                } else {
                    dpm.clearPackagePersistentPreferredActivities(admin, context.packageName)
                    packages.setComponentEnabledSetting(
                        homeComponent,
                        PackageManager.COMPONENT_ENABLED_STATE_DISABLED,
                        PackageManager.DONT_KILL_APP
                    )
                    val exitIntent = Intent(context, KylokHomeActivity::class.java).apply {
                        addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                        putExtra(KylokHomeActivity.EXTRA_EXIT, true)
                    }
                    context.startActivity(exitIntent)
                }
            }
        }
    }
    companion object {
        val CAPABILITIES = listOf("policy.managed_configuration", "policy.camera", "policy.screen_capture", "policy.maximum_time_to_lock", "policy.restrictions", "policy.permissions", "policy.kiosk")
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
