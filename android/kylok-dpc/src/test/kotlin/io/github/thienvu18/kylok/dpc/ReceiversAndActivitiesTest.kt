// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.app.Activity
import android.app.ActivityManager
import android.app.TestActivityManager
import android.app.TestNotificationManager
import android.app.admin.DevicePolicyManager
import android.app.admin.TestDevicePolicyManager
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.content.pm.PackageManager
import android.os.Bundle
import android.os.PersistableBundle
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.*
import org.junit.Before
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/** Comprehensive tests for broadcast receivers and provisioning/kiosk activities. */
class ReceiversAndActivitiesTest {
    private lateinit var context: TestContext
    private lateinit var fakeDb: TestAgentDatabase
    private lateinit var dpm: TestDevicePolicyManager
    private lateinit var notificationManager: TestNotificationManager

    @Before
    fun setUp() {
        TestAndroidKeyStoreProvider.install()
        TestAndroidKeyStoreProvider.reset()
        context = TestContext()
        fakeDb = TestAgentDatabase()

        Kylok.executor.shutdownNow()
        Kylok.executor = java.util.concurrent.Executors.newSingleThreadExecutor()

        val configField = Kylok::class.java.getDeclaredField("configuration").apply { isAccessible = true }
        val hostConfig = configField.get(Kylok)
        val frozenField = HostConfiguration::class.java.getDeclaredField("frozen").apply { isAccessible = true }
        frozenField.set(hostConfig, false)
        val optionsField = HostConfiguration::class.java.getDeclaredField("options").apply { isAccessible = true }
        optionsField.set(hostConfig, HostOptions())

        val dbField = AgentStorage::class.java.getDeclaredField("database")
        dbField.isAccessible = true
        dbField.set(AgentStorage, fakeDb)

        dpm = TestDevicePolicyManager()
        dpm.owner = true
        context.registerService(DevicePolicyManager::class.java, dpm)

        notificationManager = TestNotificationManager()
        context.registerService(android.app.NotificationManager::class.java, notificationManager)

        InstallResultReceiver.executor = java.util.concurrent.Executor { it.run() }
        androidx.work.WorkManager.initialize(context, androidx.work.Configuration.Builder().build())
    }

    @After
    fun tearDown() {
        android.os.Build.VERSION.SDK_INT = 0
        InstallResultReceiver.executor = java.util.concurrent.Executors.newSingleThreadExecutor()
        Kylok.executor.shutdownNow()
        Kylok.executor = java.util.concurrent.Executors.newSingleThreadExecutor()
        val dbField = AgentStorage::class.java.getDeclaredField("database")
        dbField.isAccessible = true
        dbField.set(AgentStorage, null)
        TestAndroidKeyStoreProvider.reset()
    }

    @Test
    fun testInstallResultReceiverEarlyReturns() {
        val receiver = InstallResultReceiver()

        // Missing operation_id -> early return
        val intent1 = Intent()
        receiver.onReceive(context, intent1)

        // Session mismatch -> early return
        AgentStorage.update(context) { state ->
            val ops = JSONObject()
            ops.put("op-1", JSONObject().put("session", 10).put("superseded", false))
            state.put("operations", ops)
        }
        val intent2 = Intent().apply {
            putExtra("operation_id", "op-1")
            putExtra("session_id", 999)
        }
        receiver.onReceive(context, intent2)
        assertEquals(10, AgentStorage.read(context).getJSONObject("operations").getJSONObject("op-1").getInt("session"))

        // Superseded -> early return
        AgentStorage.update(context) { state ->
            state.getJSONObject("operations").getJSONObject("op-1").put("superseded", true)
        }
        val intent3 = Intent().apply {
            putExtra("operation_id", "op-1")
            putExtra("session_id", 10)
            putExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_SUCCESS)
        }
        receiver.onReceive(context, intent3)
        assertFalse(AgentStorage.read(context).getJSONObject("operations").getJSONObject("op-1").has("callback"))
    }

    @Test
    fun testInstallResultReceiverSuccess() {
        AgentStorage.update(context) { state ->
            val ops = JSONObject()
            ops.put("op-1", JSONObject().put("session", 10).put("superseded", false).put("confirmation", "some-uri"))
            state.put("operations", ops)
        }

        val receiver = InstallResultReceiver()
        val intent = Intent().apply {
            putExtra("operation_id", "op-1")
            putExtra("session_id", 10)
            putExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_SUCCESS)
        }
        receiver.onReceive(context, intent)

        // Wait slightly for background executor in receiver
        Thread.sleep(100)

        val op = AgentStorage.read(context).getJSONObject("operations").getJSONObject("op-1")
        assertEquals("success", op.getString("callback"))
        assertFalse(op.has("confirmation"))
    }

    @Test
    fun testInstallResultReceiverPendingUserAction() {
        AgentStorage.update(context) { state ->
            val ops = JSONObject()
            ops.put("op-1", JSONObject().put("session", 12).put("superseded", false))
            state.put("operations", ops)
        }

        val confirmIntent = Intent("android.intent.action.VIEW").apply {
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        val receiver = InstallResultReceiver()
        val intent = Intent().apply {
            putExtra("operation_id", "op-1")
            putExtra("session_id", 12)
            putExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_PENDING_USER_ACTION)
            putExtra(Intent.EXTRA_INTENT, confirmIntent)
        }
        receiver.onReceive(context, intent)
        Thread.sleep(100)

        val op = AgentStorage.read(context).getJSONObject("operations").getJSONObject("op-1")
        assertEquals("waiting", op.getString("callback"))
        assertTrue(op.has("confirmation"))
        assertEquals(1, notificationManager.channels.size)
    }

    @Test
    fun testInstallResultReceiverFailureStatus() {
        AgentStorage.update(context) { state ->
            val ops = JSONObject()
            ops.put("op-1", JSONObject().put("session", 15).put("superseded", false).put("confirmation", "some-uri"))
            state.put("operations", ops)
        }

        val receiver = InstallResultReceiver()
        val intent = Intent().apply {
            putExtra("operation_id", "op-1")
            putExtra("session_id", 15)
            putExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE_BLOCKED)
        }
        receiver.onReceive(context, intent)
        Thread.sleep(100)

        val op = AgentStorage.read(context).getJSONObject("operations").getJSONObject("op-1")
        assertEquals("failure", op.getString("callback"))
        assertEquals("PACKAGE_INSTALLER_2", op.getString("callback_code"))
        assertFalse(op.has("confirmation"))
    }

    @Test
    fun testKylokDeviceAdminReceiverProvisioningAndLockTask() {
        val receiver = KylokDeviceAdminReceiver()

        // onProfileProvisioningComplete
        val extras = PersistableBundle().apply {
            putString("kylok.server", "http://192.168.1.100")
            putString("kylok.enrollment_token", "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOP1")
        }
        val provIntent = Intent().apply {
            putExtra(DevicePolicyManager.EXTRA_PROVISIONING_ADMIN_EXTRAS_BUNDLE, extras)
        }
        receiver.onProfileProvisioningComplete(context, provIntent)
        Thread.sleep(100)
        assertEquals("PROFILE_PROVISIONING_COMPLETE", AgentStorage.read(context).optString("callback"))

        // onEnabled
        receiver.onEnabled(context, provIntent)
        Thread.sleep(100)
        assertEquals("DEVICE_ADMIN_ENABLED", AgentStorage.read(context).optString("callback"))

        // onLockTaskModeEntering
        receiver.onLockTaskModeEntering(context, Intent(), "com.example.kiosk")
        Thread.sleep(100)
        assertEquals("com.example.kiosk", AgentStorage.read(context).getString("lock_task_package"))

        // onLockTaskModeExiting
        receiver.onLockTaskModeExiting(context, Intent())
        Thread.sleep(100)
        assertEquals("", AgentStorage.read(context).getString("lock_task_package"))
    }

    @Test
    fun testLifecycleReceiver() {
        val receiver = LifecycleReceiver()

        receiver.onReceive(context, Intent(Intent.ACTION_BOOT_COMPLETED))
        receiver.onReceive(context, Intent(Intent.ACTION_MY_PACKAGE_REPLACED))
        receiver.onReceive(context, Intent(Intent.ACTION_POWER_CONNECTED))
    }

    private fun Activity.callOnCreate(bundle: Bundle?) {
        val m = javaClass.getDeclaredMethod("onCreate", Bundle::class.java)
        m.isAccessible = true
        m.invoke(this, bundle)
    }

    private fun Activity.callOnResume() {
        val m = javaClass.getDeclaredMethod("onResume")
        m.isAccessible = true
        try {
            m.invoke(this)
        } catch (e: java.lang.reflect.InvocationTargetException) {
            e.targetException.printStackTrace()
            throw e.targetException
        }
    }

    private fun Activity.callOnNewIntent(intent: Intent) {
        val m = javaClass.getDeclaredMethod("onNewIntent", Intent::class.java)
        m.isAccessible = true
        m.invoke(this, intent)
    }

    private fun Activity.callOnDestroy() {
        val m = javaClass.getDeclaredMethod("onDestroy")
        m.isAccessible = true
        m.invoke(this)
    }

    @Test
    fun testProvisioningModeActivityFlows() {
        val activity = ProvisioningModeActivity()
        activity.attach(context)

        // Wrong action -> finishes with RESULT_CANCELED
        activity.intent = Intent("android.intent.action.VIEW")
        activity.callOnCreate(null)
        assertTrue(activity.isFinishing)
        assertEquals(Activity.RESULT_CANCELED, activity.mResultCode)

        // Allowed modes does not contain FULLY_MANAGED -> finishes CANCELED
        val act2 = ProvisioningModeActivity()
        act2.attach(context)
        act2.intent = Intent(DevicePolicyManager.ACTION_GET_PROVISIONING_MODE).apply {
            putIntegerArrayListExtra(DevicePolicyManager.EXTRA_PROVISIONING_ALLOWED_PROVISIONING_MODES, arrayListOf(2))
        }
        act2.callOnCreate(null)
        assertTrue(act2.isFinishing)
        assertEquals(Activity.RESULT_CANCELED, act2.mResultCode)

        // Fully managed with invalid extras -> finishes CANCELED
        val act3 = ProvisioningModeActivity()
        act3.attach(context)
        val badExtras = PersistableBundle().apply {
            putString("kylok.server", "http://invalid-url-with-path/path")
        }
        act3.intent = Intent(DevicePolicyManager.ACTION_GET_PROVISIONING_MODE).apply {
            putIntegerArrayListExtra(
                DevicePolicyManager.EXTRA_PROVISIONING_ALLOWED_PROVISIONING_MODES,
                arrayListOf(DevicePolicyManager.PROVISIONING_MODE_FULLY_MANAGED_DEVICE)
            )
            putExtra(DevicePolicyManager.EXTRA_PROVISIONING_ADMIN_EXTRAS_BUNDLE, badExtras)
        }
        act3.callOnCreate(null)
        Kylok.executor.submit {}.get(3, TimeUnit.SECONDS)
        var waited = 0
        while (!act3.isFinishing && waited < 3000) {
            Thread.sleep(50)
            waited += 50
        }
        assertTrue(act3.isFinishing)

        // Fully managed with valid extras -> sets RESULT_OK with mode extra
        val act4 = ProvisioningModeActivity()
        act4.attach(context)
        val goodExtras = PersistableBundle().apply {
            putString("kylok.server", "http://192.168.1.100")
            putString("kylok.enrollment_token", "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOP2")
        }
        act4.intent = Intent(DevicePolicyManager.ACTION_GET_PROVISIONING_MODE).apply {
            putIntegerArrayListExtra(
                DevicePolicyManager.EXTRA_PROVISIONING_ALLOWED_PROVISIONING_MODES,
                arrayListOf(DevicePolicyManager.PROVISIONING_MODE_FULLY_MANAGED_DEVICE)
            )
            putExtra(DevicePolicyManager.EXTRA_PROVISIONING_ADMIN_EXTRAS_BUNDLE, goodExtras)
        }
        act4.callOnCreate(null)
        Kylok.executor.submit {}.get(3, TimeUnit.SECONDS)
        waited = 0
        while (!act4.isFinishing && waited < 3000) {
            Thread.sleep(50)
            waited += 50
        }
        assertEquals(Activity.RESULT_OK, act4.mResultCode)
        assertNotNull(act4.mResultData)
        assertEquals(
            DevicePolicyManager.PROVISIONING_MODE_FULLY_MANAGED_DEVICE,
            act4.mResultData!!.getIntExtra(DevicePolicyManager.EXTRA_PROVISIONING_MODE, -1)
        )
    }

    @Test
    fun testPolicyComplianceActivityFlows() {
        val activity = PolicyComplianceActivity()
        activity.attach(context)

        // Wrong action -> finishes
        activity.intent = Intent("android.intent.action.VIEW")
        activity.callOnCreate(null)
        assertTrue(activity.isFinishing)

        // Not owner -> finishes
        val act2 = PolicyComplianceActivity()
        act2.attach(context)
        dpm.owner = false
        act2.intent = Intent(DevicePolicyManager.ACTION_ADMIN_POLICY_COMPLIANCE)
        act2.callOnCreate(null)
        assertTrue(act2.isFinishing)

        // Valid owner, extras valid, prepareLAN flow
        dpm.owner = true
        val act3 = PolicyComplianceActivity()
        act3.attach(context)
        val goodExtras = PersistableBundle().apply {
            putString("kylok.server", "http://192.168.1.100")
            putString("kylok.enrollment_token", "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOP3")
        }
        act3.intent = Intent(DevicePolicyManager.ACTION_ADMIN_POLICY_COMPLIANCE).apply {
            putExtra(DevicePolicyManager.EXTRA_PROVISIONING_ADMIN_EXTRAS_BUNDLE, goodExtras)
        }
        act3.callOnCreate(null)
        var waited = 0
        while (!act3.isFinishing && waited < 3000) {
            Thread.sleep(50)
            waited += 50
        }
        assertTrue(act3.isFinishing)
        assertEquals(Activity.RESULT_OK, act3.mResultCode)

        // onRequestPermissionsResult success & denial
        context.defaultPackageManager.permissionGrants[Kylok.LAN_PERMISSION] = PackageManager.PERMISSION_GRANTED
        val act4 = PolicyComplianceActivity()
        act4.attach(context)
        act4.onRequestPermissionsResult(1, arrayOf(Kylok.LAN_PERMISSION), intArrayOf(PackageManager.PERMISSION_GRANTED))
        assertEquals(Activity.RESULT_OK, act4.mResultCode)

        val act5 = PolicyComplianceActivity()
        act5.attach(context)
        act5.onRequestPermissionsResult(2, arrayOf(Kylok.LAN_PERMISSION), intArrayOf(PackageManager.PERMISSION_DENIED))
        assertNotNull(act5.mContentView)

        // Test when SDK_INT >= 37 and permission denied triggers prepareLAN requestPermissions
        try {
            android.os.Build.VERSION.SDK_INT = 37
            context.defaultPackageManager.permissionGrants[Kylok.LAN_PERMISSION] = PackageManager.PERMISSION_DENIED
            val actDenied = PolicyComplianceActivity()
            actDenied.attach(context)
            actDenied.intent = Intent(DevicePolicyManager.ACTION_ADMIN_POLICY_COMPLIANCE).apply {
                putExtra(DevicePolicyManager.EXTRA_PROVISIONING_ADMIN_EXTRAS_BUNDLE, goodExtras)
            }
            actDenied.callOnCreate(null)
            Kylok.executor.submit {}.get(3, TimeUnit.SECONDS)
            assertEquals(1, actDenied.mRequestedPermissionCode)
            assertArrayEquals(arrayOf(Kylok.LAN_PERMISSION), actDenied.mRequestedPermissions)

            // onRequestPermissionsResult with denial sets content view
            actDenied.onRequestPermissionsResult(1, arrayOf(Kylok.LAN_PERMISSION), intArrayOf(PackageManager.PERMISSION_DENIED))
            assertNotNull(actDenied.mContentView)
        } finally {
            android.os.Build.VERSION.SDK_INT = 0
        }
        context.defaultPackageManager.permissionGrants[Kylok.LAN_PERMISSION] = PackageManager.PERMISSION_GRANTED
        context.defaultPackageManager.permissionGrants[Kylok.LAN_PERMISSION] = PackageManager.PERMISSION_GRANTED
        act5.onRequestPermissionsResult(2, arrayOf("other"), intArrayOf(PackageManager.PERMISSION_DENIED))
        assertNotNull(act5.mContentView)
    }

    @Test
    fun testKylokHomeActivityLifecycle() {
        val activity = KylokHomeActivity()
        activity.attach(context)

        val am = TestActivityManager()
        context.registerService(ActivityManager::class.java, am)

        val keyguard = android.app.TestKeyguardManager()
        context.registerService(android.app.KeyguardManager::class.java, keyguard)

        // onCreate
        activity.callOnCreate(null)
        assertNotNull(activity.mContentView)

        // onBackPressed suppressed
        activity.onBackPressed()

        // Test dynamic broadcast receivers
        val pkgRecvField = KylokHomeActivity::class.java.getDeclaredField("packageReceiver").apply { isAccessible = true }
        val pkgRecv = pkgRecvField.get(activity) as android.content.BroadcastReceiver
        pkgRecv.onReceive(context, Intent(Intent.ACTION_PACKAGE_REPLACED))

        val userRecvField = KylokHomeActivity::class.java.getDeclaredField("userPresentReceiver").apply { isAccessible = true }
        val userRecv = userRecvField.get(activity) as android.content.BroadcastReceiver
        userRecv.onReceive(context, Intent(Intent.ACTION_USER_PRESENT))

        // Snapshot with kiosk package
        val fakeCertBytes = "cert1".toByteArray()
        val certDigest = java.security.MessageDigest.getInstance("SHA-256").digest(fakeCertBytes)
            .joinToString("") { "%02x".format(it) }
        val art = io.github.thienvu18.kylok.proto.v1.Artifact.newBuilder()
            .setPackageName("com.kiosk.app").setVersionCode(1).addSigningCertificates(certDigest).build()
        val snap = io.github.thienvu18.kylok.proto.v1.SyncResponse.newBuilder()
            .addDesiredApplications(io.github.thienvu18.kylok.proto.v1.DesiredApplication.newBuilder().setArtifact(art).setRevision(1))
            .setDesiredPolicy(io.github.thienvu18.kylok.proto.v1.DesiredPolicy.newBuilder().setRevision(1).setPolicy(
                io.github.thienvu18.kylok.proto.v1.Policy.newBuilder()
                    .addRequiredPackages("com.kiosk.app")
                    .setLockTask(io.github.thienvu18.kylok.proto.v1.LockTaskPolicy.newBuilder().addPackages("com.kiosk.app").setKioskPackage("com.kiosk.app"))
            )).build()

        AgentStorage.update(context) { it.put("snapshot", encode(snap)) }

        val kioskActivity = KylokHomeActivity()
        kioskActivity.attach(context)
        kioskActivity.callOnCreate(null)

        // PREREQUISITES_PENDING (app not installed yet)
        kioskActivity.intent = Intent()
        kioskActivity.callOnResume()

        // Install app in packageInfoMap matching art
        val kioskPi = android.content.pm.PackageInfo().apply {
            packageName = "com.kiosk.app"
            longVersionCode = 1L
            signingInfo = android.content.pm.SigningInfo(arrayOf(android.content.pm.Signature(fakeCertBytes)))
        }
        context.defaultPackageManager.packageInfoMap["com.kiosk.app"] = kioskPi
        context.defaultPackageManager.installedPackages.add(kioskPi)

        // DEVICE_LOCKED
        keyguard.deviceLocked = true
        kioskActivity.callOnResume()
        keyguard.deviceLocked = false

        // ALLOWLIST_PENDING
        dpm.lockTaskPermitted = false
        kioskActivity.callOnResume()
        dpm.lockTaskPermitted = true

        // NOT_LAUNCHABLE
        kioskActivity.callOnResume()

        // LAUNCHED
        context.defaultPackageManager.launchIntentMap["com.kiosk.app"] = Intent("kiosk.launch")
        kioskActivity.callOnResume()

        // THROTTLED
        kioskActivity.callOnResume()

        // ALREADY_RUNNING
        am.modeState = ActivityManager.LOCK_TASK_MODE_LOCKED
        AgentStorage.update(context) { it.put("lock_task_package", "com.kiosk.app") }
        kioskActivity.callOnResume()

        // LauncherRecovery.recover
        LauncherRecovery.recover(context)

        // onResume with EXTRA_EXIT
        activity.intent = Intent().apply { putExtra(KylokHomeActivity.EXTRA_EXIT, true) }
        am.modeState = ActivityManager.LOCK_TASK_MODE_LOCKED
        activity.callOnResume()
        assertTrue(activity.isFinishing)
        assertTrue(activity.mLockTaskStopped)

        // onNewIntent with EXTRA_EXIT
        val act2 = KylokHomeActivity()
        act2.attach(context)
        act2.callOnCreate(null)
        act2.callOnNewIntent(Intent().apply { putExtra(KylokHomeActivity.EXTRA_EXIT, true) })
        assertTrue(act2.isFinishing)

        // onDestroy cleans up
        act2.callOnDestroy()

        // NOT_OWNER outcome finishes
        dpm.owner = false
        val notOwnerActivity = KylokHomeActivity().apply { attach(context) }
        notOwnerActivity.callOnCreate(null)
        notOwnerActivity.callOnResume()
        assertTrue(notOwnerActivity.isFinishing)
        dpm.owner = true

        // CLEARED outcome finishes
        val clearedSnap = snap.toBuilder().setDesiredPolicy(snap.desiredPolicy.toBuilder().setPolicy(
            snap.desiredPolicy.policy.toBuilder().setLockTask(io.github.thienvu18.kylok.proto.v1.LockTaskPolicy.getDefaultInstance())
        )).build()
        AgentStorage.update(context) { it.put("snapshot", encode(clearedSnap)) }
        val clearedActivity = KylokHomeActivity().apply { attach(context) }
        clearedActivity.callOnCreate(null)
        clearedActivity.callOnResume()
        assertTrue(clearedActivity.isFinishing)

        // backCallback invocation
        val backField = KylokHomeActivity::class.java.getDeclaredField("backCallback").apply { isAccessible = true }
        val backCb = backField.get(activity) as android.window.OnBackInvokedCallback
        backCb.onBackInvoked()

        // Null state finishes
        val oldRec = fakeDb.recordRef.get()
        try {
            fakeDb.recordRef.set(io.github.thienvu18.kylok.dpc.storage.AgentRecord().apply {
                id = 1
                sealed = byteArrayOf(1, 2, 3)
            })
            val corruptActivity = KylokHomeActivity().apply { attach(context) }
            corruptActivity.callOnCreate(null)
            corruptActivity.callOnResume()
            assertTrue(corruptActivity.isFinishing)
        } finally {
            fakeDb.recordRef.set(oldRec)
        }
    }
}
