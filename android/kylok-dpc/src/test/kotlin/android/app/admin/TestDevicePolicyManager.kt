// SPDX-License-Identifier: LGPL-3.0-only
package android.app.admin

import android.content.ComponentName
import android.content.IntentFilter
import android.os.Bundle

/** Subclass of DevicePolicyManager for deterministic unit testing of device policy enforcement. */
class TestDevicePolicyManager : DevicePolicyManager() {
    var owner: Boolean = true
    var cameraDisabled: Boolean = false
    var screenCaptureDisabled: Boolean = false
    var maxTimeToLock: Long = 0L
    val userRestrictions = mutableSetOf<String>()
    val globalUserRestrictions = mutableSetOf<String>()
    val lockTaskPackages = mutableListOf<String>()
    var lockTaskPermitted: Boolean = true
    var sensorGrantAllowed: Boolean = true
    val permissionGrants = mutableMapOf<String, Int>()
    val appRestrictions = mutableMapOf<String, Bundle>()
    val uninstalledBlockedMap = mutableMapOf<String, Boolean>()
    val caCerts = mutableSetOf<String>()
    var installCaResult: Boolean = true
    var setPermissionResult: Boolean = true

    override fun isDeviceOwnerApp(packageName: String?): Boolean = owner

    override fun getCameraDisabled(admin: ComponentName?): Boolean = cameraDisabled
    override fun setCameraDisabled(admin: ComponentName?, disabled: Boolean) { cameraDisabled = disabled }

    override fun getScreenCaptureDisabled(admin: ComponentName?): Boolean = screenCaptureDisabled
    override fun setScreenCaptureDisabled(admin: ComponentName?, disabled: Boolean) { screenCaptureDisabled = disabled }

    override fun getMaximumTimeToLock(admin: ComponentName?): Long = maxTimeToLock
    override fun setMaximumTimeToLock(admin: ComponentName?, timeMs: Long) { maxTimeToLock = timeMs }

    override fun addUserRestriction(admin: ComponentName?, key: String?) {
        if (key != null) userRestrictions.add(key)
    }

    override fun clearUserRestriction(admin: ComponentName?, key: String?) {
        if (key != null) userRestrictions.remove(key)
    }

    override fun getUserRestrictions(admin: ComponentName): Bundle {
        val bundle = Bundle()
        for (key in userRestrictions) {
            bundle.putBoolean(key, true)
        }
        return bundle
    }

    override fun getUserRestrictionsGlobally(): Bundle {
        val bundle = Bundle()
        for (key in (userRestrictions + globalUserRestrictions)) {
            bundle.putBoolean(key, true)
        }
        return bundle
    }

    override fun setLockTaskPackages(admin: ComponentName?, packages: Array<String>) {
        lockTaskPackages.clear()
        lockTaskPackages.addAll(packages)
    }

    override fun getLockTaskPackages(admin: ComponentName?): Array<String> = lockTaskPackages.toTypedArray()

    override fun isLockTaskPermitted(pkg: String?): Boolean = lockTaskPermitted

    override fun isUninstallBlocked(admin: ComponentName?, packageName: String?): Boolean =
        uninstalledBlockedMap[packageName ?: ""] ?: false

    override fun setUninstallBlocked(admin: ComponentName?, packageName: String?, uninstalledBlocked: Boolean) {
        if (packageName != null) uninstalledBlockedMap[packageName] = uninstalledBlocked
    }

    override fun getPermissionGrantState(admin: ComponentName?, packageName: String, permission: String): Int =
        permissionGrants["$packageName:$permission"] ?: DevicePolicyManager.PERMISSION_GRANT_STATE_DEFAULT

    override fun setPermissionGrantState(admin: ComponentName?, packageName: String, permission: String, grantState: Int): Boolean {
        if (!setPermissionResult) return false
        permissionGrants["$packageName:$permission"] = grantState
        return true
    }

    override fun getApplicationRestrictions(admin: ComponentName?, packageName: String?): Bundle =
        appRestrictions[packageName ?: ""] ?: Bundle()

    override fun setApplicationRestrictions(admin: ComponentName?, packageName: String?, settings: Bundle?) {
        if (packageName != null) {
            appRestrictions[packageName] = settings ?: Bundle()
        }
    }

    override fun addPersistentPreferredActivity(admin: ComponentName?, filter: IntentFilter?, activity: ComponentName) {}

    override fun clearPackagePersistentPreferredActivities(admin: ComponentName?, packageName: String?) {}

    override fun installCaCert(admin: ComponentName?, certBuffer: ByteArray?): Boolean {
        if (!installCaResult) return false
        if (certBuffer != null) caCerts.add(java.util.Base64.getEncoder().encodeToString(certBuffer))
        return true
    }

    override fun uninstallCaCert(admin: ComponentName?, certBuffer: ByteArray?) {
        if (certBuffer != null) caCerts.remove(java.util.Base64.getEncoder().encodeToString(certBuffer))
    }

    override fun hasCaCertInstalled(admin: ComponentName?, certBuffer: ByteArray?): Boolean =
        certBuffer != null && caCerts.contains(java.util.Base64.getEncoder().encodeToString(certBuffer))

    override fun canAdminGrantSensorsPermissions(): Boolean = sensorGrantAllowed
}
