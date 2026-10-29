// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.app.Activity
import android.app.ActivityManager
import android.app.KeyguardManager
import android.app.admin.DevicePolicyManager
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.pm.*
import android.net.ConnectivityManager
import android.net.wifi.WifiConfiguration
import android.net.wifi.WifiInfo
import android.net.wifi.WifiManager
import android.os.Build
import android.os.Bundle
import android.os.UserManager
import io.github.thienvu18.kylok.proto.v1.*
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.File
import java.lang.reflect.Field
import java.net.URL

/** Verify Android platform adapter contracts across command, launcher, policy, install, and network implementations. */
@Suppress("DEPRECATION")
class AndroidPlatformAdaptersTest {

    private fun newInstance(className: String): Any {
        val clazz = Class.forName(className)
        val ctor = clazz.declaredConstructors[0].apply { isAccessible = true }
        val params = ctor.parameterTypes.map { type ->
            when (type) {
                Boolean::class.javaPrimitiveType -> false
                Int::class.javaPrimitiveType -> 0
                Long::class.javaPrimitiveType -> 0L
                else -> null
            }
        }.toTypedArray()
        return ctor.newInstance(*params)
    }

    private fun setField(target: Any, fieldName: String, value: Any?) {
        var cls: Class<*>? = target.javaClass
        while (cls != null) {
            try {
                val field: Field = cls.getDeclaredField(fieldName)
                field.isAccessible = true
                field.set(target, value)
                return
            } catch (_: NoSuchFieldException) {
                cls = cls.superclass
            }
        }
        throw NoSuchFieldException(fieldName)
    }

    @Test
    fun testCommandPlatformLifecycleAndActions() {
        val dpm = newInstance("android.app.admin.DevicePolicyManager") as DevicePolicyManager
        val kg = newInstance("android.app.KeyguardManager") as KeyguardManager
        val ctx = TestContext().apply {
            registerService(DevicePolicyManager::class.java, dpm)
            registerService(KeyguardManager::class.java, kg)
        }
        val platform = AndroidCommandPlatform(ctx)
        setField(platform, "dpm", dpm)
        setField(platform, "keyguard", kg)
        assertTrue(platform.now() > 0)
        assertNotNull(platform.processID())
        assertFalse(platform.isOwner())
        assertFalse(platform.isSecure())
        assertFalse(platform.isLocked())
        platform.reboot()
        platform.lock()
        assertEquals(listOf("command.reboot", "command.lock"), AndroidCommandPlatform.CAPABILITIES)
    }

    @Test
    fun testLauncherPlatformMethods() {
        val dpm = newInstance("android.app.admin.DevicePolicyManager") as DevicePolicyManager
        val kg = newInstance("android.app.KeyguardManager") as KeyguardManager
        val am = newInstance("android.app.ActivityManager") as ActivityManager
        val state = MemoryState(JSONObject())
        val ctx = TestContext().apply {
            registerService(DevicePolicyManager::class.java, dpm)
            registerService(KeyguardManager::class.java, kg)
            registerService(ActivityManager::class.java, am)
        }
        val platform = AndroidLauncherPlatform(ctx, state)
        setField(platform, "dpm", dpm)
        setField(platform, "activityManager", am)
        setField(platform, "keyguard", kg)

        assertTrue(platform.now() > 0)
        assertFalse(platform.isOwner())
        assertFalse(platform.isDeviceLocked())
        assertFalse(platform.isLockTaskPermitted("com.test.app"))
        assertFalse(platform.isLockTaskLocked())
        assertFalse(platform.isHomePreferred())

        platform.setHomePreferred(true)
        platform.setHomePreferred(false)
        assertNull(platform.installedApp("com.test.app"))
        assertFalse(platform.isAppLaunchable("com.test.app"))

        platform.launchAppInLockTask("com.test.app")
        platform.finishLauncher()
        assertEquals(1, ctx.startedActivities.size)

        val activityCtx = object : Activity() {}.apply { attach(ctx) }
        val platformActivity = AndroidLauncherPlatform(activityCtx, state)
        platformActivity.finishLauncher()
        assertTrue(activityCtx.isFinishing)

        // isHomePreferred true
        ctx.defaultPackageManager.resolveActivityHandler = java.util.function.BiFunction { _, _ ->
            ResolveInfo().apply {
                activityInfo = ActivityInfo().apply { packageName = ctx.packageName; name = KylokHomeActivity::class.java.name }
            }
        }
        assertTrue(platform.isHomePreferred())
        ctx.defaultPackageManager.resolveActivityHandler = null

        // isAppLaunchable via queryIntentActivities
        ctx.defaultPackageManager.queryIntentActivitiesHandler = java.util.function.BiFunction { intent, _ ->
            if (intent.hasCategory(Intent.CATEGORY_HOME)) {
                listOf(ResolveInfo().apply { activityInfo = ActivityInfo().apply { packageName = "com.kiosk.home"; name = "HomeAct" } })
            } else emptyList()
        }
        assertTrue(platform.isAppLaunchable("com.kiosk.home"))

        // launchAppInLockTask via queryIntentActivities
        platform.launchAppInLockTask("com.kiosk.home")

        platformActivity.updateRecovery("com.kiosk.pkg", 2, 5000L, 1000L)
        val recovery = state.read().getJSONObject("launcher_recovery")
        assertEquals("com.kiosk.pkg", recovery.getString("kiosk_package"))
        assertEquals(2, recovery.getInt("crash_count"))
        assertEquals(5000L, recovery.getLong("throttled_until"))
    }

    @Test
    fun testPolicyPlatformLifecycleAndEnforcement() {
        val dpm = android.app.admin.TestDevicePolicyManager()
        val kg = android.app.TestKeyguardManager()
        val am = android.app.TestActivityManager()
        val um = android.os.TestUserManager()
        val state = MemoryState(JSONObject())
        val ctx = TestContext().apply {
            registerService(DevicePolicyManager::class.java, dpm)
            registerService(KeyguardManager::class.java, kg)
            registerService(ActivityManager::class.java, am)
            registerService(UserManager::class.java, um)
        }
        val platform = AndroidPolicyPlatform(ctx, state)
        setField(platform, "dpm", dpm)
        setField(platform, "activityManager", am)
        setField(platform, "keyguard", kg)
        setField(platform, "userManager", um)

        assertTrue(platform.now() > 0)
        assertTrue(platform.isOwner())
        assertEquals(AndroidPolicyPlatform.CAPABILITIES.size, 7)

        // Uninstall blocked
        platform.setUninstallBlocked("com.test.target", true)
        assertTrue(platform.isUninstallBlocked("com.test.target"))
        dpm.owner = false
        platform.setUninstallBlocked("com.test.target", false)
        assertFalse(platform.isUninstallBlocked("com.test.target"))
        dpm.owner = true

        // check() when not owner
        dpm.owner = false
        assertThrows(SecurityException::class.java) { platform.check(PolicySetting("r1", PolicyKind.CAMERA, "true")) }
        dpm.owner = true

        // check() RESTRICTION
        assertThrows(PolicyUnsupported::class.java) { platform.check(PolicySetting("r1", PolicyKind.RESTRICTION, "true", name = "unknown_restriction")) }
        platform.check(PolicySetting("r1", PolicyKind.RESTRICTION, "true", name = UserManager.DISALLOW_ADD_USER))

        // check() MANAGED_CONFIGURATION
        assertThrows(PolicyPending::class.java) { platform.check(PolicySetting("r2", PolicyKind.MANAGED_CONFIGURATION, "{}", target = "com.uninstalled.app")) }
        val testAppInfo = PackageInfo().apply {
            packageName = "com.installed.app"
            applicationInfo = ApplicationInfo().apply { packageName = "com.installed.app"; targetSdkVersion = 33 }
            requestedPermissions = arrayOf("android.permission.CAMERA")
        }
        ctx.defaultPackageManager.packageInfoMap["com.installed.app"] = testAppInfo
        val validConfig = ConfigurationValues.journal(ConfigurationBundle.getDefaultInstance())
        platform.check(PolicySetting("r2", PolicyKind.MANAGED_CONFIGURATION, validConfig, target = "com.installed.app"))

        // check() PERMISSION
        assertThrows(PolicyPending::class.java) { platform.check(PolicySetting("r3", PolicyKind.PERMISSION, "GRANTED", target = "com.missing.app", name = "android.permission.CAMERA")) }
        assertThrows(PolicyUnsupported::class.java) { platform.check(PolicySetting("r3", PolicyKind.PERMISSION, "GRANTED", target = "com.installed.app", name = "android.permission.UNKNOWN")) }

        val camPerm = object : PermissionInfo() {
            override fun getProtection(): Int = PermissionInfo.PROTECTION_DANGEROUS
        }.apply {
            name = "android.permission.CAMERA"
            protectionLevel = PermissionInfo.PROTECTION_DANGEROUS
        }
        ctx.defaultPackageManager.permissionInfoMap["android.permission.CAMERA"] = camPerm

        val oldSdkApp = PackageInfo().apply {
            packageName = "com.old.app"
            applicationInfo = ApplicationInfo().apply { packageName = "com.old.app"; targetSdkVersion = 22 }
            requestedPermissions = arrayOf("android.permission.CAMERA")
        }
        ctx.defaultPackageManager.packageInfoMap["com.old.app"] = oldSdkApp
        assertThrows(PolicyUnsupported::class.java) { platform.check(PolicySetting("r3", PolicyKind.PERMISSION, "GRANTED", target = "com.old.app", name = "android.permission.CAMERA")) }

        dpm.sensorGrantAllowed = false
        assertThrows(PolicyUnsupported::class.java) { platform.check(PolicySetting("r3", PolicyKind.PERMISSION, "GRANTED", target = "com.installed.app", name = "android.permission.CAMERA")) }

        // SDK 36 health permission branch
        android.os.Build.VERSION.SDK_INT = 36
        try {
            val healthPerm = object : PermissionInfo() {
                override fun getProtection(): Int = PermissionInfo.PROTECTION_DANGEROUS
            }.apply {
                name = "android.permission.health.READ_HEART_RATE"
                protectionLevel = PermissionInfo.PROTECTION_DANGEROUS
            }
            ctx.defaultPackageManager.permissionInfoMap["android.permission.health.READ_HEART_RATE"] = healthPerm
            val healthApp = PackageInfo().apply {
                packageName = "com.health.app"
                applicationInfo = ApplicationInfo().apply { packageName = "com.health.app"; targetSdkVersion = 34 }
                requestedPermissions = arrayOf("android.permission.health.READ_HEART_RATE")
            }
            ctx.defaultPackageManager.packageInfoMap["com.health.app"] = healthApp
            assertThrows(PolicyUnsupported::class.java) {
                platform.check(PolicySetting("r_health", PolicyKind.PERMISSION, "GRANTED", target = "com.health.app", name = "android.permission.health.READ_HEART_RATE"))
            }
        } finally {
            android.os.Build.VERSION.SDK_INT = 0
        }
        dpm.sensorGrantAllowed = true

        assertThrows(PolicyUnsupported::class.java) { platform.check(PolicySetting("r3", PolicyKind.PERMISSION, "INVALID_STATE", target = "com.installed.app", name = "android.permission.CAMERA")) }
        platform.check(PolicySetting("r3", PolicyKind.PERMISSION, "GRANTED", target = "com.installed.app", name = "android.permission.CAMERA"))

        // capture() & observe() CAMERA
        dpm.cameraDisabled = false
        assertEquals("false", platform.capture(PolicySetting("r1", PolicyKind.CAMERA, "true")))
        assertEquals("false", platform.observe(PolicySetting("r1", PolicyKind.CAMERA, "true")))

        // capture() & observe() SCREEN_CAPTURE
        dpm.screenCaptureDisabled = true
        assertEquals("true", platform.capture(PolicySetting("r2", PolicyKind.SCREEN_CAPTURE, "true")))
        assertEquals("true", platform.observe(PolicySetting("r2", PolicyKind.SCREEN_CAPTURE, "true")))

        // capture() & observe() MAXIMUM_TIME_TO_LOCK
        dpm.maxTimeToLock = 60000L
        assertEquals("60000", platform.capture(PolicySetting("r3", PolicyKind.MAXIMUM_TIME_TO_LOCK, "60000")))
        assertEquals("60000", platform.observe(PolicySetting("r3", PolicyKind.MAXIMUM_TIME_TO_LOCK, "60000")))

        // capture() & observe() RESTRICTION
        dpm.userRestrictions.add(UserManager.DISALLOW_ADD_USER)
        assertEquals("true", platform.capture(PolicySetting("r4", PolicyKind.RESTRICTION, "true", name = UserManager.DISALLOW_ADD_USER)))
        um.restrictions.add(UserManager.DISALLOW_ADD_USER)
        assertEquals("true", platform.observe(PolicySetting("r4", PolicyKind.RESTRICTION, "true", name = UserManager.DISALLOW_ADD_USER)))

        // SDK 34 global restrictions branch
        android.os.Build.VERSION.SDK_INT = 34
        try {
            dpm.globalUserRestrictions.add(UserManager.DISALLOW_AIRPLANE_MODE)
            assertEquals("true", platform.capture(PolicySetting("r_global", PolicyKind.RESTRICTION, "true", name = UserManager.DISALLOW_AIRPLANE_MODE)))
        } finally {
            android.os.Build.VERSION.SDK_INT = 0
        }

        // DISALLOW_UNINSTALL_APPS in observe()
        um.restrictions.remove(UserManager.DISALLOW_UNINSTALL_APPS)
        assertEquals("false", platform.observe(PolicySetting("r4", PolicyKind.RESTRICTION, "true", name = UserManager.DISALLOW_UNINSTALL_APPS)))
        um.restrictions.add(UserManager.DISALLOW_UNINSTALL_APPS)
        ctx.defaultPackageManager.installedPackages.add(PackageInfo().apply {
            packageName = "com.user.app"
            applicationInfo = ApplicationInfo().apply { flags = 0 }
        })
        assertEquals("UNINSTALL_BLOCKED_PENDING", platform.observe(PolicySetting("r4", PolicyKind.RESTRICTION, "true", name = UserManager.DISALLOW_UNINSTALL_APPS)))
        dpm.uninstalledBlockedMap["com.user.app"] = true
        assertEquals("true", platform.observe(PolicySetting("r4", PolicyKind.RESTRICTION, "true", name = UserManager.DISALLOW_UNINSTALL_APPS)))

        // capture() & observe() PERMISSION
        dpm.permissionGrants["com.installed.app:android.permission.CAMERA"] = DevicePolicyManager.PERMISSION_GRANT_STATE_GRANTED
        assertEquals("GRANTED", platform.capture(PolicySetting("r5", PolicyKind.PERMISSION, "GRANTED", target = "com.installed.app", name = "android.permission.CAMERA")))
        ctx.defaultPackageManager.permissionGrants["com.installed.app:android.permission.CAMERA"] = PackageManager.PERMISSION_DENIED
        assertEquals("GRANTED_NOT_EFFECTIVE", platform.observe(PolicySetting("r5", PolicyKind.PERMISSION, "GRANTED", target = "com.installed.app", name = "android.permission.CAMERA")))
        ctx.defaultPackageManager.permissionGrants["com.installed.app:android.permission.CAMERA"] = PackageManager.PERMISSION_GRANTED
        assertEquals("GRANTED", platform.observe(PolicySetting("r5", PolicyKind.PERMISSION, "GRANTED", target = "com.installed.app", name = "android.permission.CAMERA")))

        // permissionState capture with DENIED and DEFAULT
        dpm.permissionGrants["com.installed.app:android.permission.CAMERA"] = DevicePolicyManager.PERMISSION_GRANT_STATE_DENIED
        assertEquals("DENIED", platform.capture(PolicySetting("r5", PolicyKind.PERMISSION, "DENIED", target = "com.installed.app", name = "android.permission.CAMERA")))

        dpm.permissionGrants["com.installed.app:android.permission.CAMERA"] = DevicePolicyManager.PERMISSION_GRANT_STATE_DEFAULT
        assertEquals("DEFAULT", platform.capture(PolicySetting("r5", PolicyKind.PERMISSION, "DEFAULT", target = "com.installed.app", name = "android.permission.CAMERA")))

        dpm.permissionGrants["com.installed.app:android.permission.CAMERA"] = 999
        assertEquals("unknown", platform.capture(PolicySetting("r5", PolicyKind.PERMISSION, "DEFAULT", target = "com.installed.app", name = "android.permission.CAMERA")))

        // capture() & observe() MANAGED_CONFIGURATION
        assertEquals(validConfig, platform.capture(PolicySetting("r6", PolicyKind.MANAGED_CONFIGURATION, validConfig, target = "com.installed.app")))
        assertEquals(validConfig, platform.observe(PolicySetting("r6", PolicyKind.MANAGED_CONFIGURATION, validConfig, target = "com.installed.app")))

        // capture() & observe() LOCK_TASK
        dpm.lockTaskPackages.addAll(listOf("com.kiosk.app"))
        val lockTaskVal = JSONObject().put("packages", JSONArray(listOf("com.kiosk.app"))).put("kiosk_package", "com.kiosk.app").toString()
        val capturedLock = platform.capture(PolicySetting("r7", PolicyKind.LOCK_TASK, lockTaskVal))
        assertTrue(capturedLock.contains("com.kiosk.app"))

        // observe() LOCK_TASK variations
        val observedEmptyLock = platform.observe(PolicySetting("r7", PolicyKind.LOCK_TASK, lockTaskVal))
        assertTrue(observedEmptyLock.contains("\"kiosk_package\":\"\""))

        ctx.defaultPackageManager.resolveActivityHandler = java.util.function.BiFunction { _, _ ->
            ResolveInfo().apply {
                activityInfo = ActivityInfo().apply { packageName = ctx.packageName; name = KylokHomeActivity::class.java.name }
            }
        }
        val observedLauncherNotLocked = platform.observe(PolicySetting("r7", PolicyKind.LOCK_TASK, lockTaskVal))
        assertTrue(observedLauncherNotLocked.contains("\"kiosk_package\":\"LAUNCHER_PREFERRED_NOT_LOCKED\""))

        ctx.defaultPackageManager.resolveActivityHandler = null
        am.modeState = ActivityManager.LOCK_TASK_MODE_LOCKED
        val observedLockedNoLauncher = platform.observe(PolicySetting("r7", PolicyKind.LOCK_TASK, lockTaskVal))
        assertTrue(observedLockedNoLauncher.contains("\"kiosk_package\":\"LOCKED_WITHOUT_LAUNCHER\""))

        ctx.defaultPackageManager.resolveActivityHandler = java.util.function.BiFunction { _, _ ->
            ResolveInfo().apply {
                activityInfo = ActivityInfo().apply { packageName = ctx.packageName; name = KylokHomeActivity::class.java.name }
            }
        }
        val observedLockedUnknown = platform.observe(PolicySetting("r7", PolicyKind.LOCK_TASK, lockTaskVal))
        assertTrue(observedLockedUnknown.contains("\"kiosk_package\":\"unknown\""))

        am.modeState = ActivityManager.LOCK_TASK_MODE_NONE
        ctx.defaultPackageManager.resolveActivityHandler = null

        // apply() CAMERA, SCREEN_CAPTURE, MAXIMUM_TIME_TO_LOCK
        platform.apply(PolicySetting("r1", PolicyKind.CAMERA, "true"))
        assertTrue(dpm.cameraDisabled)
        platform.apply(PolicySetting("r2", PolicyKind.SCREEN_CAPTURE, "true"))
        assertTrue(dpm.screenCaptureDisabled)
        platform.apply(PolicySetting("r3", PolicyKind.MAXIMUM_TIME_TO_LOCK, "30000"))
        assertEquals(30000L, dpm.maxTimeToLock)

        // apply() RESTRICTION
        platform.apply(PolicySetting("r4", PolicyKind.RESTRICTION, "true", name = UserManager.DISALLOW_ADD_USER))
        assertTrue(dpm.userRestrictions.contains(UserManager.DISALLOW_ADD_USER))
        platform.apply(PolicySetting("r4", PolicyKind.RESTRICTION, "false", name = UserManager.DISALLOW_ADD_USER))
        assertFalse(dpm.userRestrictions.contains(UserManager.DISALLOW_ADD_USER))

        // apply() DISALLOW_UNINSTALL_APPS with requiredPackages snapshot
        val snapWithReq = io.github.thienvu18.kylok.proto.v1.SyncResponse.newBuilder()
            .setDesiredPolicy(io.github.thienvu18.kylok.proto.v1.DesiredPolicy.newBuilder().setRevision(1).setPolicy(
                io.github.thienvu18.kylok.proto.v1.Policy.newBuilder().addRequiredPackages("com.user.app")
            )).build()
        state.update { it.put("snapshot", encode(snapWithReq)) }
        platform.apply(PolicySetting("r4", PolicyKind.RESTRICTION, "true", name = UserManager.DISALLOW_UNINSTALL_APPS))
        assertTrue(dpm.uninstalledBlockedMap["com.user.app"] == true)
        platform.apply(PolicySetting("r4", PolicyKind.RESTRICTION, "false", name = UserManager.DISALLOW_UNINSTALL_APPS))
        // Since com.user.app is in requiredPackages, it remains blocked
        assertTrue(dpm.uninstalledBlockedMap["com.user.app"] == true)

        // apply() MANAGED_CONFIGURATION
        platform.apply(PolicySetting("r6", PolicyKind.MANAGED_CONFIGURATION, validConfig, target = "com.installed.app"))

        // apply() PERMISSION
        platform.apply(PolicySetting("r5", PolicyKind.PERMISSION, "GRANTED", target = "com.installed.app", name = "android.permission.CAMERA"))
        assertEquals(DevicePolicyManager.PERMISSION_GRANT_STATE_GRANTED, dpm.permissionGrants["com.installed.app:android.permission.CAMERA"])
        platform.apply(PolicySetting("r5", PolicyKind.PERMISSION, "DENIED", target = "com.installed.app", name = "android.permission.CAMERA"))
        assertEquals(DevicePolicyManager.PERMISSION_GRANT_STATE_DENIED, dpm.permissionGrants["com.installed.app:android.permission.CAMERA"])
        platform.apply(PolicySetting("r5", PolicyKind.PERMISSION, "DEFAULT", target = "com.installed.app", name = "android.permission.CAMERA"))
        assertEquals(DevicePolicyManager.PERMISSION_GRANT_STATE_DEFAULT, dpm.permissionGrants["com.installed.app:android.permission.CAMERA"])

        dpm.setPermissionResult = false
        assertThrows(PolicyUnsupported::class.java) { platform.apply(PolicySetting("r5", PolicyKind.PERMISSION, "GRANTED", target = "com.installed.app", name = "android.permission.CAMERA")) }
        dpm.setPermissionResult = true

        // apply() LOCK_TASK with kiosk app
        ctx.defaultPackageManager.launchIntentMap["com.kiosk.app"] = Intent("kiosk.launch")
        kg.deviceLocked = true
        assertThrows(PolicyPending::class.java) { platform.apply(PolicySetting("r7", PolicyKind.LOCK_TASK, lockTaskVal)) }
        kg.deviceLocked = false

        dpm.lockTaskPermitted = false
        assertThrows(PolicyPending::class.java) { platform.apply(PolicySetting("r7", PolicyKind.LOCK_TASK, lockTaskVal)) }
        dpm.lockTaskPermitted = true

        ctx.defaultPackageManager.launchIntentMap.remove("com.kiosk.app")
        assertThrows(PolicyUnsupported::class.java) { platform.apply(PolicySetting("r7", PolicyKind.LOCK_TASK, lockTaskVal)) }

        ctx.defaultPackageManager.launchIntentMap["com.kiosk.app"] = Intent("kiosk.launch")
        platform.apply(PolicySetting("r7", PolicyKind.LOCK_TASK, lockTaskVal))

        // apply() LOCK_TASK with empty kiosk (exit kiosk)
        val exitLockTaskVal = JSONObject().put("packages", JSONArray(emptyList<String>())).put("kiosk_package", "").toString()
        platform.apply(PolicySetting("r7", PolicyKind.LOCK_TASK, exitLockTaskVal))

        // apply() LOCK_TASK with home activity resolved via queryIntentActivities
        ctx.defaultPackageManager.launchIntentMap.remove("com.kiosk.app")
        ctx.defaultPackageManager.queryIntentActivitiesHandler = java.util.function.BiFunction { _, _ ->
            listOf(ResolveInfo().apply {
                activityInfo = ActivityInfo().apply { packageName = "com.kiosk.app"; name = "com.kiosk.app.HomeActivity" }
            })
        }
        platform.apply(PolicySetting("r7", PolicyKind.LOCK_TASK, lockTaskVal))
        ctx.defaultPackageManager.queryIntentActivitiesHandler = null

        // apply() LOCK_TASK with home activity resolved via queryIntentActivities
        ctx.defaultPackageManager.launchIntentMap.remove("com.kiosk.app")
        ctx.defaultPackageManager.queryIntentActivitiesHandler = java.util.function.BiFunction { _, _ ->
            listOf(ResolveInfo().apply {
                activityInfo = ActivityInfo().apply { packageName = "com.kiosk.app"; name = "com.kiosk.app.HomeActivity" }
            })
        }
        platform.apply(PolicySetting("r7", PolicyKind.LOCK_TASK, lockTaskVal))
        ctx.defaultPackageManager.queryIntentActivitiesHandler = null

    }

    @Test
    fun testInstallPlatformLifecycleAndActions() {
        val sm = android.os.storage.TestStorageManager()
        val am = android.app.TestActivityManager()
        val dpm = android.app.admin.TestDevicePolicyManager().apply { owner = false }
        val ctx = TestContext().apply {
            registerService(android.os.storage.StorageManager::class.java, sm)
            registerService(ActivityManager::class.java, am)
            registerService(DevicePolicyManager::class.java, dpm)
        }
        val state = MemoryState(JSONObject().put("server", "http://localhost:8080").put("credential", "cred").put("device_id", "dev1"))
        val platform = AndroidInstallPlatform(ctx, state)

        assertTrue(platform.now() > 0)
        assertFalse(platform.isOwner())

        val op = Operation.newBuilder()
            .setId("op-1")
            .setDownloadUrl("/artifacts/op-1/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef.apk")
            .setLeaseId("lease-1")
            .setArtifact(Artifact.newBuilder()
                .setPackageName("com.test.pkg")
                .setVersionCode(10)
                .setSizeBytes(1024)
                .setSha256("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
                .setMinSdk(21)
                .setMaxSdk(36)
                .build())
            .build()

        // download with insufficient storage
        sm.allocatableBytes = 100 // too small
        assertThrows(InstallRejected::class.java) { platform.download(op) {} }
        sm.allocatableBytes = 100 * 1024 * 1024 // 100 MiB

        // verify SDK mismatch
        val opHighSdk = op.toBuilder().setArtifact(op.artifact.toBuilder().setMinSdk(37).build()).build()
        assertThrows(InstallRejected::class.java) { platform.verify(opHighSdk) }

        val opLowSdk = op.toBuilder().setArtifact(op.artifact.toBuilder().setMaxSdk(20).build()).build()
        assertThrows(InstallRejected::class.java) { platform.verify(opLowSdk) }

        // verify ABI mismatch
        val opAbi = op.toBuilder().setArtifact(op.artifact.toBuilder().addSupportedAbis("fake_abi_xyz").build()).build()
        assertThrows(InstallRejected::class.java) { platform.verify(opAbi) }

        // sessions
        val sessId = platform.createSession(op)
        assertTrue(sessId > 0)
        assertTrue(platform.sessionExists(sessId))
        assertFalse(platform.sessionSealed(sessId))

        // write session
        val stagingDir = File(ctx.noBackupFilesDir, "kylok-apks")
        val apkFile = File(stagingDir, "${op.id}.apk").apply {
            parentFile?.mkdirs()
            writeBytes(ByteArray(1024))
        }
        platform.writeSession(sessId, op)

        // download: insufficient storage
        sm.allocatableBytes = 10L
        assertThrows(InstallRejected::class.java) { platform.download(op) {} }
        sm.allocatableBytes = 1024L * 1024 * 1024

        // download: already present with valid size & digest succeeds
        val apkBytes = ByteArray(1024) { (it % 251).toByte() }
        val apkSha256 = java.security.MessageDigest.getInstance("SHA-256").digest(apkBytes).joinToString("") { "%02x".format(it) }
        val validOp = op.toBuilder().setArtifact(op.artifact.toBuilder().setSha256(apkSha256).setSizeBytes(1024)).setDownloadUrl("/artifacts/${op.id}/$apkSha256.apk").build()
        val validApkFile = File(stagingDir, "${validOp.id}.apk").apply { writeBytes(apkBytes) }
        platform.download(validOp) {}

        // verify() edge cases
        ctx.defaultPackageManager.packageArchiveInfoMap[validApkFile.path] = null
        assertThrows(InstallRejected::class.java) { platform.verify(validOp) }

        val badNameInfo = PackageInfo().apply { packageName = "wrong.pkg"; longVersionCode = 10 }
        ctx.defaultPackageManager.packageArchiveInfoMap[validApkFile.path] = badNameInfo
        assertThrows(InstallRejected::class.java) { platform.verify(validOp) }

        val splitInfo = PackageInfo().apply { packageName = validOp.artifact.packageName; longVersionCode = 10; splitNames = arrayOf("split1") }
        ctx.defaultPackageManager.packageArchiveInfoMap[validApkFile.path] = splitInfo
        assertThrows(InstallRejected::class.java) { platform.verify(validOp) }

        val missingFeatInfo = PackageInfo().apply {
            packageName = validOp.artifact.packageName
            longVersionCode = 10
            reqFeatures = arrayOf(FeatureInfo().apply { name = "feat.missing"; flags = FeatureInfo.FLAG_REQUIRED })
        }
        ctx.defaultPackageManager.packageArchiveInfoMap[validApkFile.path] = missingFeatInfo
        assertThrows(InstallRejected::class.java) { platform.verify(validOp) }

        val glesInfo = PackageInfo().apply {
            packageName = validOp.artifact.packageName
            longVersionCode = 10
            reqFeatures = arrayOf(FeatureInfo().apply { reqGlEsVersion = 0x00030002; flags = FeatureInfo.FLAG_REQUIRED })
        }
        ctx.defaultPackageManager.packageArchiveInfoMap[validApkFile.path] = glesInfo
        assertThrows(InstallRejected::class.java) { platform.verify(validOp) }

        // Downgrade check
        val downgradeInfo = PackageInfo().apply { packageName = validOp.artifact.packageName; longVersionCode = 10 }
        ctx.defaultPackageManager.packageArchiveInfoMap[validApkFile.path] = downgradeInfo
        ctx.defaultPackageManager.packageInfoMap[validOp.artifact.packageName] = PackageInfo().apply {
            packageName = validOp.artifact.packageName
            longVersionCode = 20
        }
        assertThrows(InstallRejected::class.java) { platform.verify(validOp) }

        // Signing mismatch
        ctx.defaultPackageManager.packageInfoMap[validOp.artifact.packageName] = PackageInfo().apply {
            packageName = validOp.artifact.packageName
            longVersionCode = 10
            signingInfo = SigningInfo()
        }
        assertThrows(InstallRejected::class.java) { platform.verify(validOp) }
        ctx.defaultPackageManager.packageInfoMap.remove(validOp.artifact.packageName)

        // Verify succeeds with matching ABI and certificates
        val fakeCertBytes = byteArrayOf(10, 20, 30)
        val fakeCertDigest = java.security.MessageDigest.getInstance("SHA-256").digest(fakeCertBytes).joinToString("") { "%02x".format(it) }
        val validPkgInfo = PackageInfo().apply {
            packageName = validOp.artifact.packageName
            longVersionCode = validOp.artifact.versionCode
            signingInfo = SigningInfo(arrayOf(Signature(fakeCertBytes)))
        }
        ctx.defaultPackageManager.packageArchiveInfoMap[validApkFile.path] = validPkgInfo
        val opAbiMatch = validOp.toBuilder().setArtifact(validOp.artifact.toBuilder().addSupportedAbis("arm64-v8a").addSigningCertificates(fakeCertDigest)).build()
        android.os.Build.VERSION.SDK_INT = 28
        try {
            platform.verify(opAbiMatch)
        } finally {
            android.os.Build.VERSION.SDK_INT = 0
        }

        platform.commit(sessId, op.id)
        assertTrue(platform.sessionExists(sessId))

        platform.abandon(sessId)
        platform.cleanup(setOf("op-1"), setOf(sessId))
        assertTrue(apkFile.exists())
        platform.cleanup(emptySet(), emptySet())
        assertFalse(apkFile.exists())

        // installed() returns null for unknown package
        assertNull(platform.installed("com.nonexistent"))

        // installed() returns InstalledApplication with certificates
        val fakePkgInfo = PackageInfo().apply {
            packageName = "com.test.pkg"
            versionCode = 10
            longVersionCode = 10L
            signingInfo = SigningInfo(arrayOf(Signature(fakeCertBytes)))
        }
        ctx.defaultPackageManager.packageInfoMap["com.test.pkg"] = fakePkgInfo
        val installedApp = platform.installed("com.test.pkg")
        assertNotNull(installedApp)
        assertEquals("com.test.pkg", installedApp!!.packageName)
        assertEquals(10L, installedApp.versionCode)
        assertEquals(listOf(fakeCertDigest), installedApp.signingCertificatesList)

        // Staging a valid APK file with correct SHA-256
        val contentBytes = ByteArray(10)
        val actualSha256 = java.security.MessageDigest.getInstance("SHA-256").digest(contentBytes).joinToString("") { "%02x".format(it) }
        val opMatchingSha = op.toBuilder()
            .setDownloadUrl("/artifacts/${op.id}/$actualSha256.apk")
            .setArtifact(op.artifact.toBuilder()
                .setSha256(actualSha256)
                .setSizeBytes(10)
                .setMinSdk(0)
                .setMaxSdk(0)
                .addAllSigningCertificates(listOf(fakeCertDigest))
                .build()).build()
        val validFile = File(stagingDir, "${opMatchingSha.id}.apk").apply { writeBytes(contentBytes) }

        // verify: APK_PARSE_FAILED if not in packageArchiveInfoMap
        ctx.defaultPackageManager.packageArchiveInfoMap.remove(validFile.path)
        assertThrows(InstallRejected::class.java) { platform.verify(opMatchingSha) }

        // verify: APK_METADATA_MISMATCH
        val archiveInfoBad = PackageInfo().apply {
            packageName = "wrong.pkg"
            versionCode = 10
            longVersionCode = 10L
            signingInfo = SigningInfo(arrayOf(Signature(fakeCertBytes)))
        }
        ctx.defaultPackageManager.packageArchiveInfoMap[validFile.path] = archiveInfoBad
        assertThrows(InstallRejected::class.java) { platform.verify(opMatchingSha) }

        // verify: SPLIT_APK_UNSUPPORTED
        val archiveInfoSplit = PackageInfo().apply {
            packageName = opMatchingSha.artifact.packageName
            versionCode = opMatchingSha.artifact.versionCode.toInt()
            longVersionCode = opMatchingSha.artifact.versionCode
            signingInfo = SigningInfo(arrayOf(Signature(fakeCertBytes)))
            splitNames = arrayOf("split.config")
        }
        ctx.defaultPackageManager.packageArchiveInfoMap[validFile.path] = archiveInfoSplit
        assertThrows(InstallRejected::class.java) { platform.verify(opMatchingSha) }

        // verify: MISSING_REQUIRED_FEATURE
        val archiveInfoFeature = PackageInfo().apply {
            packageName = opMatchingSha.artifact.packageName
            versionCode = opMatchingSha.artifact.versionCode.toInt()
            longVersionCode = opMatchingSha.artifact.versionCode
            signingInfo = SigningInfo(arrayOf(Signature(fakeCertBytes)))
            reqFeatures = arrayOf(FeatureInfo().apply {
                name = "missing.hardware.feature"
                flags = FeatureInfo.FLAG_REQUIRED
            })
        }
        ctx.defaultPackageManager.packageArchiveInfoMap[validFile.path] = archiveInfoFeature
        assertThrows(InstallRejected::class.java) { platform.verify(opMatchingSha) }

        // verify: INCOMPATIBLE_OPENGL
        val archiveInfoGles = PackageInfo().apply {
            packageName = opMatchingSha.artifact.packageName
            versionCode = opMatchingSha.artifact.versionCode.toInt()
            longVersionCode = opMatchingSha.artifact.versionCode
            signingInfo = SigningInfo(arrayOf(Signature(fakeCertBytes)))
            reqFeatures = arrayOf(FeatureInfo().apply {
                reqGlEsVersion = 0x00030000 // higher than 0x00020000
                flags = FeatureInfo.FLAG_REQUIRED
            })
        }
        ctx.defaultPackageManager.packageArchiveInfoMap[validFile.path] = archiveInfoGles
        assertThrows(InstallRejected::class.java) { platform.verify(opMatchingSha) }

        // verify: DOWNGRADE_REJECTED (installed version > apk version)
        fakePkgInfo.versionCode = 20
        fakePkgInfo.longVersionCode = 20L
        val archiveInfoGood = PackageInfo().apply {
            packageName = opMatchingSha.artifact.packageName
            versionCode = opMatchingSha.artifact.versionCode.toInt()
            longVersionCode = opMatchingSha.artifact.versionCode
            signingInfo = SigningInfo(arrayOf(Signature(fakeCertBytes)))
        }
        ctx.defaultPackageManager.packageArchiveInfoMap[validFile.path] = archiveInfoGood
        assertThrows(InstallRejected::class.java) { platform.verify(opMatchingSha) }
        fakePkgInfo.versionCode = 5
        fakePkgInfo.longVersionCode = 5L // reset to lower

        // verify: SIGNING_IDENTITY_MISMATCH
        val otherCertBytes = byteArrayOf(99, 98, 97)
        val otherPkgInfo = PackageInfo().apply {
            packageName = "com.test.pkg"
            versionCode = 5
            longVersionCode = 5L
            signingInfo = SigningInfo(arrayOf(Signature(otherCertBytes)))
        }
        ctx.defaultPackageManager.packageInfoMap["com.test.pkg"] = otherPkgInfo
        assertThrows(InstallRejected::class.java) { platform.verify(opMatchingSha) }

        // verify: Lineage match with signing certificate history
        val archiveInfoWithHistory = PackageInfo().apply {
            packageName = opMatchingSha.artifact.packageName
            versionCode = opMatchingSha.artifact.versionCode.toInt()
            longVersionCode = opMatchingSha.artifact.versionCode
            signingInfo = SigningInfo(arrayOf(Signature(fakeCertBytes))).apply {
                signingCertificateHistory = arrayOf(Signature(otherCertBytes), Signature(fakeCertBytes))
                multipleSigners = false
            }
        }
        ctx.defaultPackageManager.packageArchiveInfoMap[validFile.path] = archiveInfoWithHistory
        platform.verify(opMatchingSha) // succeeds!

        // cleanup with abandoned sessions
        val installer = ctx.defaultPackageManager.testInstaller
        installer.sessionInfos[100] = android.content.pm.TestPackageInstallerSessionInfo(100, android.net.Uri.parse("kylok://install/op-old"), false)
        installer.sessionInfos[101] = android.content.pm.TestPackageInstallerSessionInfo(101, android.net.Uri.parse("kylok://install/op-retained"), false)
        installer.sessionInfos[102] = android.content.pm.TestPackageInstallerSessionInfo(102, android.net.Uri.parse("kylok://install/op-sealed"), true)
        installer.sessionInfos[103] = android.content.pm.TestPackageInstallerSessionInfo(103, android.net.Uri.parse("https://other.com"), false)
        platform.cleanup(setOf("op-retained"), setOf(101))
        assertTrue(100 in installer.abandonedSessions)
        assertFalse(101 in installer.abandonedSessions)
        assertFalse(102 in installer.abandonedSessions)
        assertFalse(103 in installer.abandonedSessions)
    }

    @Test
    fun testNetworkPlatformLifecycleAndActions() {
        val wifi = android.net.wifi.TestWifiManager()
        val connectivity = android.net.TestConnectivityManager()
        val dpm = android.app.admin.TestDevicePolicyManager()
        val ctx = TestContext().apply {
            registerService(WifiManager::class.java, wifi)
            registerService(ConnectivityManager::class.java, connectivity)
            registerService(DevicePolicyManager::class.java, dpm)
        }
        val state = MemoryState(JSONObject().put("server", "http://localhost:8080").put("credential", "cred").put("device_id", "dev1"))
        val platform = AndroidNetworkPlatform(ctx, state)
        setField(platform, "wifi", wifi)
        setField(platform, "connectivity", connectivity)
        setField(platform, "dpm", dpm)

        assertTrue(platform.now() > 0)
        assertEquals(AndroidNetworkPlatform.CAPABILITIES, listOf("network.lifecycle"))

        // check() when not owner
        dpm.owner = false
        assertThrows(NetworkRejected::class.java) { platform.check() }
        dpm.owner = true

        // check() when wifi disabled
        wifi.wifiEnabled = false
        assertThrows(NetworkRejected::class.java) { platform.check() }
        wifi.wifiEnabled = true

        platform.check() // succeeds

        // configured & callerConfigured
        val netConfig = WifiConfiguration().apply {
            networkId = 1
            SSID = "\"TestNet\""
            allowedKeyManagement = java.util.BitSet().apply { set(WifiConfiguration.KeyMgmt.WPA_PSK) }
            wepKeys = arrayOfNulls(4)
        }
        wifi.savedConfigs.add(netConfig)
        wifi.callerConfigs.add(netConfig)

        val configured = platform.configured()
        assertEquals(1, configured.size)
        assertEquals("TestNet", configured[0].ssid)
        assertEquals("WPA2_PSK", configured[0].security)

        val callerConfigured = platform.callerConfigured()
        assertEquals(1, callerConfigured.size)
        assertEquals(1, platform.currentNetworkID())

        // upsert unmanaged collision
        val profile = WifiProfile.newBuilder().setSsid("TestNet").setSecurity("WPA2_PSK").build()
        assertThrows(NetworkRejected::class.java) { platform.upsert(profile, null) }

        // upsert new OPEN network
        val openProfile = WifiProfile.newBuilder().setSsid("NewOpenNet").setSecurity("OPEN").build()
        val newId = platform.upsert(openProfile, null)
        assertTrue(newId >= 0)

        // upsert update existing caller network
        val updateProfile = WifiProfile.newBuilder().setSsid("TestNet").setSecurity("WPA2_PSK").setPassphrase(com.google.protobuf.ByteString.copyFromUtf8("password123")).build()
        val updatedId = platform.upsert(updateProfile, 1)
        assertEquals(1, updatedId)

        // select
        platform.select(1)

        // remove
        // removal blocked if active
        val saved = SavedNetwork(1, "TestNet", "WPA2_PSK")
        assertThrows(NetworkRejected::class.java) { platform.remove(saved) }

        // removal succeeds when not active
        wifi.connInfo = android.net.wifi.TestWifiInfo(netId = 2)
        platform.remove(saved)

        // CA certs
        val fakeCert = byteArrayOf(1, 2, 3, 4)
        assertFalse(platform.hasCA(fakeCert))
        platform.installCA(fakeCert)
        assertTrue(platform.hasCA(fakeCert))
        platform.removeCA(fakeCert)
        assertFalse(platform.hasCA(fakeCert))

        // selectedNetwork
        assertNull(platform.selectedNetwork(-1))
        assertNull(platform.selectedNetwork(999)) // mismatch current

        // check() permissions
        ctx.permissionGrants[android.Manifest.permission.ACCESS_FINE_LOCATION] = PackageManager.PERMISSION_DENIED
        assertThrows(NetworkRejected::class.java) { platform.check() }
        ctx.permissionGrants[android.Manifest.permission.ACCESS_FINE_LOCATION] = PackageManager.PERMISSION_GRANTED

        ctx.permissionGrants[android.Manifest.permission.NEARBY_WIFI_DEVICES] = PackageManager.PERMISSION_DENIED
        assertThrows(NetworkRejected::class.java) { platform.check() }
        ctx.permissionGrants[android.Manifest.permission.NEARBY_WIFI_DEVICES] = PackageManager.PERMISSION_GRANTED

        // security(config) branches: WPA3_SAE, OTHER
        val wpa3Config = WifiConfiguration().apply {
            networkId = 2
            SSID = "\"Wpa3Net\""
            allowedKeyManagement = java.util.BitSet().apply { set(WifiConfiguration.KeyMgmt.SAE) }
        }
        val otherConfig = WifiConfiguration().apply {
            networkId = 3
            SSID = "\"OtherNet\""
            allowedKeyManagement = java.util.BitSet().apply { set(WifiConfiguration.KeyMgmt.NONE) }
            wepKeys = arrayOf("wepKey1", null, null, null)
        }
        wifi.savedConfigs.add(wpa3Config)
        wifi.callerConfigs.add(wpa3Config)
        wifi.savedConfigs.add(otherConfig)
        wifi.callerConfigs.add(otherConfig)

        val allConfigs = platform.configured()
        assertTrue(allConfigs.any { it.ssid == "Wpa3Net" && it.security == "WPA3_SAE" })
        assertTrue(allConfigs.any { it.ssid == "OtherNet" && it.security == "OTHER" })

        // ssid unobservable
        val nullSsidConfig = WifiConfiguration().apply { networkId = 4; SSID = null }
        wifi.savedConfigs.add(nullSsidConfig)
        assertThrows(NetworkRejected::class.java) { platform.configured() }
        wifi.savedConfigs.remove(nullSsidConfig)

        // upsert: NETWORK_OWNERSHIP_CHANGED
        assertThrows(NetworkRejected::class.java) { platform.upsert(profile, 999) }

        // upsert: WPA2_PSK with 64-hex passphrase
        val hex64Pass = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
        val hexProfile = WifiProfile.newBuilder().setSsid("HexNet").setSecurity("WPA2_PSK")
            .setPassphrase(com.google.protobuf.ByteString.copyFromUtf8(hex64Pass)).build()
        val hexId = platform.upsert(hexProfile, null)
        assertTrue(hexId >= 0)

        // upsert: update network with null bitsets
        val nullBitsetConfig = WifiConfiguration().apply {
            networkId = 10
            SSID = "\"NullBitsetNet\""
            allowedProtocols = null
            allowedPairwiseCiphers = null
            allowedGroupCiphers = null
        }
        wifi.callerConfigs.add(nullBitsetConfig)
        val updatePskProfile = WifiProfile.newBuilder().setSsid("NullBitsetNet").setSecurity("WPA2_PSK")
            .setPassphrase(com.google.protobuf.ByteString.copyFromUtf8("password123")).build()
        val bitsetId = platform.upsert(updatePskProfile, 10)
        assertEquals(10, bitsetId)

        // upsert: WPA3_SAE supported vs unsupported
        val wpa3Profile = WifiProfile.newBuilder().setSsid("NewWpa3Net").setSecurity("WPA3_SAE")
            .setPassphrase(com.google.protobuf.ByteString.copyFromUtf8("pass12345")).build()
        wifi.wpa3Supported = false
        assertThrows(NetworkRejected::class.java) { platform.upsert(wpa3Profile, null) }
        wifi.wpa3Supported = true
        val wpa3Id = platform.upsert(wpa3Profile, null)
        assertTrue(wpa3Id >= 0)

        // upsert: SECURITY_UNSUPPORTED
        val invalidSecProfile = WifiProfile.newBuilder().setSsid("InvNet").setSecurity("UNKNOWN_SEC").build()
        assertThrows(NetworkRejected::class.java) { platform.upsert(invalidSecProfile, null) }

        // upsert: NETWORK_ENABLE_REJECTED
        wifi.enableNetworkResult = false
        val failEnableProfile = WifiProfile.newBuilder().setSsid("FailEnable").setSecurity("OPEN").build()
        assertThrows(NetworkRejected::class.java) { platform.upsert(failEnableProfile, null) }
        wifi.enableNetworkResult = true

        // select: NETWORK_SELECTION_REJECTED
        assertThrows(NetworkRejected::class.java) { platform.select(9999) }
        wifi.enableNetworkResult = false
        assertThrows(NetworkRejected::class.java) { platform.select(1) }
        wifi.enableNetworkResult = true

        // remove: error branches
        val unknownSaved = SavedNetwork(9999, "UnknownNet", "OPEN")
        assertThrows(NetworkRejected::class.java) { platform.remove(unknownSaved) }

        wifi.connInfo = android.net.wifi.TestWifiInfo(netId = -1)
        assertThrows(NetworkRejected::class.java) { platform.remove(SavedNetwork(1, "TestNet", "WPA2_PSK")) }

        wifi.connInfo = android.net.wifi.TestWifiInfo(netId = 2)
        wifi.removeNetworkResult = false
        assertThrows(NetworkRejected::class.java) { platform.remove(SavedNetwork(1, "TestNet", "WPA2_PSK")) }
        wifi.removeNetworkResult = true

        // remove: configured still contains network -> NETWORK_RELEASE_PENDING
        val stickyConfig = WifiConfiguration().apply { networkId = 20; SSID = "\"StickyNet\"" }
        wifi.savedConfigs.add(stickyConfig)
        wifi.callerConfigs.add(stickyConfig)
        assertThrows(NetworkRejected::class.java) { platform.remove(SavedNetwork(20, "StickyNet", "WPA2_PSK")) }
        wifi.savedConfigs.remove(stickyConfig)
        wifi.callerConfigs.remove(stickyConfig)

        // trustsCAByDefault
        assertFalse(platform.trustsCAByDefault(byteArrayOf(1, 2, 3)))

        // installCA failure
        dpm.installCaResult = false
        assertThrows(NetworkRejected::class.java) { platform.installCA(byteArrayOf(5, 6, 7)) }
        dpm.installCaResult = true

        // selectedNetwork with TransportInfo matching id
        val testNet = android.net.TestNetwork()
        val caps = android.net.NetworkCapabilities().apply {
            transportInfo = android.net.wifi.TestWifiInfo(netId = 1)
        }
        connectivity.networkList.add(testNet)
        connectivity.capabilitiesMap[testNet] = caps
        wifi.connInfo = android.net.wifi.TestWifiInfo(netId = 1)
        assertSame(testNet, platform.selectedNetwork(1))

        // selectedNetwork singleOrNull fallback when exact is null
        val singleNet = android.net.TestNetwork()
        val capsNoInfo = android.net.NetworkCapabilities().apply {
            wifiTransport = true
        }
        connectivity.networkList.clear()
        connectivity.networkList.add(singleNet)
        connectivity.capabilitiesMap[singleNet] = capsNoInfo
        wifi.connInfo = android.net.wifi.TestWifiInfo(netId = 1)
        assertSame(singleNet, platform.selectedNetwork(1))

        // openConnection with network_current
        state.update { it.put("network_current", JSONObject().put("id", 1)) }
        val conn = platform.openConnection(URL("http://localhost:8080/path"))
        assertNotNull(conn)
        state.update {
            it.remove("network_current")
            it.remove("network_transaction")
        }
        val plainConn = platform.openConnection(URL("http://localhost:8080/path"))
        assertNotNull(plainConn)

        // openConnection with HTTPS and NetworkTrust
        val httpsConn = platform.openConnection(URL("https://localhost:8080/path"))
        assertNotNull(httpsConn)

        // probe: security mismatch returns false immediately
        val polMismatch = NetworkPolicy.newBuilder().addProfiles(WifiProfile.newBuilder().setSsid("TestNet").setSecurity("WPA3_SAE").build()).build()
        assertFalse(platform.probe(1, polMismatch))

        val polUnknown = NetworkPolicy.newBuilder().addProfiles(WifiProfile.newBuilder().setSsid("TestNet").setSecurity("UNKNOWN").build()).build()
        assertFalse(platform.probe(1, polUnknown))

        val polOther = NetworkPolicy.newBuilder().addProfiles(WifiProfile.newBuilder().setSsid("TestNet").setSecurity("OTHER").build()).build()
        assertFalse(platform.probe(1, polOther))

        // probe: HTTP 204 success path
        val mockHttp = object : java.net.HttpURLConnection(URL("http://localhost/network-probe")) {
            val headers = mutableMapOf<String, String>()
            override fun connect() {}
            override fun disconnect() {}
            override fun usingProxy() = false
            override fun setRequestProperty(key: String, value: String) {
                headers[key] = value
                if (key == "X-Kylok-Probe") {
                    headers["X-Kylok-Probe"] = value
                }
            }
            override fun getResponseCode() = java.net.HttpURLConnection.HTTP_NO_CONTENT
            override fun getHeaderField(name: String): String? = headers[name]
        }
        val mockNet = android.net.TestNetwork { mockHttp }
        connectivity.networkList.clear()
        connectivity.networkList.add(mockNet)
        connectivity.capabilitiesMap[mockNet] = caps
        state.update {
            it.put("server", "http://localhost")
                .put("credential", "test-token")
                .put("device_id", "dev-1")
        }
        val polProbe = NetworkPolicy.newBuilder().build()
        assertTrue(platform.probe(1, polProbe))

        // probe: HttpsURLConnection branch
        val mockHttps = object : javax.net.ssl.HttpsURLConnection(URL("https://localhost/network-probe")) {
            val headers = mutableMapOf<String, String>()
            override fun connect() {}
            override fun disconnect() {}
            override fun usingProxy() = false
            override fun setRequestProperty(key: String, value: String) {
                headers[key] = value
            }
            override fun getResponseCode() = java.net.HttpURLConnection.HTTP_NO_CONTENT
            override fun getHeaderField(name: String): String? = headers[name]
            override fun getCipherSuite() = "TLS_AES_128_GCM_SHA256"
            override fun getLocalCertificates() = null
            override fun getServerCertificates() = arrayOf<java.security.cert.Certificate>()
        }
        val mockHttpsNet = android.net.TestNetwork { mockHttps }
        connectivity.networkList.clear()
        connectivity.networkList.add(mockHttpsNet)
        connectivity.capabilitiesMap[mockHttpsNet] = caps
        state.update { it.put("server", "https://localhost") }
        assertTrue(platform.probe(1, polProbe))

        // probe: IOException branch
        val mockThrowHttp = object : java.net.HttpURLConnection(URL("http://localhost/network-probe")) {
            override fun connect() { throw java.io.IOException("Network down") }
            override fun disconnect() {}
            override fun usingProxy() = false
            override fun getResponseCode(): Int { throw java.io.IOException("Network down") }
        }
        val mockThrowNet = android.net.TestNetwork { mockThrowHttp }
        connectivity.networkList.clear()
        connectivity.networkList.add(mockThrowNet)
        connectivity.capabilitiesMap[mockThrowNet] = caps
        assertFalse(platform.probe(1, polProbe))

        wifi.wifiEnabled = false
        assertThrows(NetworkRejected::class.java) { platform.check() }
        wifi.wifiEnabled = true

        // configured() variations: location denied, null configuredNetworks
        ctx.permissionGrants[android.Manifest.permission.ACCESS_FINE_LOCATION] = PackageManager.PERMISSION_DENIED
        assertThrows(NetworkRejected::class.java) { platform.configured() }
        ctx.permissionGrants.remove(android.Manifest.permission.ACCESS_FINE_LOCATION)

        wifi.returnNullConfiguredNetworks = true
        assertThrows(NetworkRejected::class.java) { platform.configured() }
        wifi.returnNullConfiguredNetworks = false

        // upsert() unmanaged SSID collision and write rejected
        wifi.savedConfigs.add(WifiConfiguration().apply { networkId = 55; SSID = "\"CollidingNet\"" })
        val collidingProf = WifiProfile.newBuilder().setSsid("CollidingNet").setSecurity("OPEN").build()
        assertThrows(NetworkRejected::class.java) { platform.upsert(collidingProf, null) }
        wifi.savedConfigs.removeIf { it.networkId == 55 }

        wifi.updateNetworkResult = -1
        assertThrows(NetworkRejected::class.java) { platform.upsert(profile, 1) }
        wifi.updateNetworkResult = null

        // probe: OPEN, WPA2_PSK, WPA3_SAE matching security types
        connectivity.networkList.clear()
        connectivity.networkList.add(mockNet)
        connectivity.capabilitiesMap[mockNet] = caps
        state.update { it.put("server", "http://localhost") }

        wifi.savedConfigs.add(WifiConfiguration().apply { networkId = 1; SSID = "\"TestNet\"" })

        wifi.connInfo = android.net.wifi.TestWifiInfo(netId = 1, secType = android.net.wifi.WifiInfo.SECURITY_TYPE_OPEN)
        val polOpen = NetworkPolicy.newBuilder().addProfiles(WifiProfile.newBuilder().setSsid("TestNet").setSecurity("OPEN").build()).build()
        assertTrue(platform.probe(1, polOpen))

        wifi.connInfo = android.net.wifi.TestWifiInfo(netId = 1, secType = android.net.wifi.WifiInfo.SECURITY_TYPE_PSK)
        val polPsk = NetworkPolicy.newBuilder().addProfiles(WifiProfile.newBuilder().setSsid("TestNet").setSecurity("WPA2_PSK").build()).build()
        assertTrue(platform.probe(1, polPsk))

        wifi.connInfo = android.net.wifi.TestWifiInfo(netId = 1, secType = android.net.wifi.WifiInfo.SECURITY_TYPE_SAE)
        val polSae = NetworkPolicy.newBuilder().addProfiles(WifiProfile.newBuilder().setSsid("TestNet").setSecurity("WPA3_SAE").build()).build()
        assertTrue(platform.probe(1, polSae))
    }
}

