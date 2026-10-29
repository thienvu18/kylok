// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.content.Context
import android.content.ContextWrapper
import android.os.Bundle
import android.os.Parcelable
import io.github.thienvu18.kylok.proto.v1.*
import org.junit.Assert.*
import org.junit.Test

/** Verify bidirectional translation between Android Bundle and normalized protobuf representations. */
class AndroidConfigurationsTest {
    @Test
    fun testCommandPlatform() {
        val dpmCtor = android.app.admin.DevicePolicyManager::class.java.declaredConstructors[0].apply { isAccessible = true }
        val dpm = dpmCtor.newInstance() as android.app.admin.DevicePolicyManager
        val kgCtor = android.app.KeyguardManager::class.java.declaredConstructors[0].apply { isAccessible = true }
        val keyguard = kgCtor.newInstance() as android.app.KeyguardManager

        val ctx = object : android.content.Context() {
            override fun getPackageName(): String = "io.github.thienvu18.kylok.dpc"
            override fun getSystemServiceName(serviceClass: Class<*>): String = serviceClass.name
            override fun getSystemService(name: String): Any? = when (name) {
                android.app.admin.DevicePolicyManager::class.java.name -> dpm
                android.app.KeyguardManager::class.java.name -> keyguard
                else -> null
            }
            override fun getAssets() = null
            override fun getResources() = null
            override fun getPackageManager() = null
            override fun getContentResolver() = null
            override fun getMainLooper() = null
            override fun getApplicationContext() = this
            override fun setTheme(resid: Int) {}
            override fun getTheme() = null
            override fun getClassLoader() = javaClass.classLoader
            override fun getApplicationInfo() = null
            override fun getPackageResourcePath() = null
            override fun getPackageCodePath() = null
            override fun getSharedPreferences(name: String?, mode: Int) = null
            override fun moveSharedPreferencesFrom(sourceContext: android.content.Context?, name: String?) = false
            override fun deleteSharedPreferences(name: String?) = false
            override fun openFileInput(name: String?) = null
            override fun openFileOutput(name: String?, mode: Int) = null
            override fun deleteFile(name: String?) = false
            override fun getFileStreamPath(name: String?) = null
            override fun getDataDir() = null
            override fun getFilesDir() = null
            override fun getNoBackupFilesDir() = null
            override fun getExternalFilesDir(type: String?) = null
            override fun getExternalFilesDirs(type: String?) = emptyArray<java.io.File>()
            override fun getObbDir() = null
            override fun getObbDirs() = emptyArray<java.io.File>()
            override fun getCacheDir() = null
            override fun getCodeCacheDir() = null
            override fun getExternalCacheDir() = null
            override fun getExternalCacheDirs() = emptyArray<java.io.File>()
            override fun getExternalMediaDirs() = emptyArray<java.io.File>()
            override fun fileList() = emptyArray<String>()
            override fun getDir(name: String?, mode: Int) = null
            override fun openOrCreateDatabase(name: String?, mode: Int, factory: android.database.sqlite.SQLiteDatabase.CursorFactory?) = null
            override fun openOrCreateDatabase(name: String?, mode: Int, factory: android.database.sqlite.SQLiteDatabase.CursorFactory?, errorHandler: android.database.DatabaseErrorHandler?) = null
            override fun moveDatabaseFrom(sourceContext: android.content.Context?, name: String?) = false
            override fun deleteDatabase(name: String?) = false
            override fun getDatabasePath(name: String?) = null
            override fun databaseList() = emptyArray<String>()
            override fun getWallpaper() = null
            override fun peekWallpaper() = null
            override fun getWallpaperDesiredMinimumWidth() = 0
            override fun getWallpaperDesiredMinimumHeight() = 0
            override fun setWallpaper(bitmap: android.graphics.Bitmap?) {}
            override fun setWallpaper(data: java.io.InputStream?) {}
            override fun clearWallpaper() {}
            override fun startActivity(intent: android.content.Intent?) {}
            override fun startActivity(intent: android.content.Intent?, options: android.os.Bundle?) {}
            override fun startActivities(intents: Array<out android.content.Intent>?) {}
            override fun startActivities(intents: Array<out android.content.Intent>?, options: android.os.Bundle?) {}
            override fun startIntentSender(intent: android.content.IntentSender?, fillInIntent: android.content.Intent?, flagsMask: Int, flagsValues: Int, extraFlags: Int) {}
            override fun startIntentSender(intent: android.content.IntentSender?, fillInIntent: android.content.Intent?, flagsMask: Int, flagsValues: Int, extraFlags: Int, options: android.os.Bundle?) {}
            override fun sendBroadcast(intent: android.content.Intent?) {}
            override fun sendBroadcast(intent: android.content.Intent?, receiverPermission: String?) {}
            override fun sendOrderedBroadcast(intent: android.content.Intent?, receiverPermission: String?) {}
            override fun sendOrderedBroadcast(intent: android.content.Intent, receiverPermission: String?, resultReceiver: android.content.BroadcastReceiver?, scheduler: android.os.Handler?, initialCode: Int, initialData: String?, initialExtras: android.os.Bundle?) {}
            override fun sendBroadcastAsUser(intent: android.content.Intent?, user: android.os.UserHandle?) {}
            override fun sendBroadcastAsUser(intent: android.content.Intent?, user: android.os.UserHandle?, receiverPermission: String?) {}
            override fun sendOrderedBroadcastAsUser(intent: android.content.Intent?, user: android.os.UserHandle?, receiverPermission: String?, resultReceiver: android.content.BroadcastReceiver?, scheduler: android.os.Handler?, initialCode: Int, initialData: String?, initialExtras: android.os.Bundle?) {}
            override fun sendStickyBroadcast(intent: android.content.Intent?) {}
            override fun sendStickyOrderedBroadcast(intent: android.content.Intent?, resultReceiver: android.content.BroadcastReceiver?, scheduler: android.os.Handler?, initialCode: Int, initialData: String?, initialExtras: android.os.Bundle?) {}
            override fun removeStickyBroadcast(intent: android.content.Intent?) {}
            override fun sendStickyBroadcastAsUser(intent: android.content.Intent?, user: android.os.UserHandle?) {}
            override fun sendStickyOrderedBroadcastAsUser(intent: android.content.Intent?, user: android.os.UserHandle?, resultReceiver: android.content.BroadcastReceiver?, scheduler: android.os.Handler?, initialCode: Int, initialData: String?, initialExtras: android.os.Bundle?) {}
            override fun removeStickyBroadcastAsUser(intent: android.content.Intent?, user: android.os.UserHandle?) {}
            override fun registerReceiver(receiver: android.content.BroadcastReceiver?, filter: android.content.IntentFilter?) = null
            override fun registerReceiver(receiver: android.content.BroadcastReceiver?, filter: android.content.IntentFilter?, flags: Int) = null
            override fun registerReceiver(receiver: android.content.BroadcastReceiver?, filter: android.content.IntentFilter?, broadcastPermission: String?, scheduler: android.os.Handler?) = null
            override fun registerReceiver(receiver: android.content.BroadcastReceiver?, filter: android.content.IntentFilter?, broadcastPermission: String?, scheduler: android.os.Handler?, flags: Int) = null
            override fun unregisterReceiver(receiver: android.content.BroadcastReceiver?) {}
            override fun startService(service: android.content.Intent?) = null
            override fun startForegroundService(service: android.content.Intent?) = null
            override fun stopService(service: android.content.Intent?) = false
            override fun bindService(service: android.content.Intent, conn: android.content.ServiceConnection, flags: Int) = false
            override fun unbindService(conn: android.content.ServiceConnection) {}
            override fun startInstrumentation(className: android.content.ComponentName, profileFile: String?, arguments: android.os.Bundle?) = false
            override fun checkPermission(permission: String, pid: Int, uid: Int) = 0
            override fun checkCallingPermission(permission: String) = 0
            override fun checkCallingOrSelfPermission(permission: String) = 0
            override fun checkSelfPermission(permission: String) = 0
            override fun enforcePermission(permission: String, pid: Int, uid: Int, message: String?) {}
            override fun enforceCallingPermission(permission: String, message: String?) {}
            override fun enforceCallingOrSelfPermission(permission: String, message: String?) {}
            override fun grantUriPermission(toPackage: String?, uri: android.net.Uri?, modeFlags: Int) {}
            override fun revokeUriPermission(uri: android.net.Uri?, modeFlags: Int) {}
            override fun revokeUriPermission(toPackage: String?, uri: android.net.Uri?, modeFlags: Int) {}
            override fun checkUriPermission(uri: android.net.Uri?, pid: Int, uid: Int, modeFlags: Int) = 0
            override fun checkCallingUriPermission(uri: android.net.Uri?, modeFlags: Int) = 0
            override fun checkCallingOrSelfUriPermission(uri: android.net.Uri?, modeFlags: Int) = 0
            override fun checkUriPermission(uri: android.net.Uri?, readPermission: String?, writePermission: String?, pid: Int, uid: Int, modeFlags: Int) = 0
            override fun enforceUriPermission(uri: android.net.Uri?, pid: Int, uid: Int, modeFlags: Int, message: String?) {}
            override fun enforceUriPermission(p0: android.net.Uri?, p1: String?, p2: String?, p3: Int, p4: Int, p5: Int, p6: String?) {}
            override fun enforceCallingUriPermission(uri: android.net.Uri?, modeFlags: Int, message: String?) {}
            override fun enforceCallingOrSelfUriPermission(uri: android.net.Uri?, modeFlags: Int, message: String?) {}
            override fun createPackageContext(packageName: String?, flags: Int) = null
            override fun createContextForSplit(splitName: String?) = null
            override fun createConfigurationContext(overrideConfiguration: android.content.res.Configuration) = null
            override fun createDisplayContext(display: android.view.Display) = null
            override fun isRestricted() = false
            override fun createDeviceProtectedStorageContext() = null
            override fun isDeviceProtectedStorage() = false
        }
        val platform = AndroidCommandPlatform(ctx)
        val dpmField = AndroidCommandPlatform::class.java.getDeclaredField("dpm").apply { isAccessible = true }
        dpmField.set(platform, dpm)
        val kgField = AndroidCommandPlatform::class.java.getDeclaredField("keyguard").apply { isAccessible = true }
        kgField.set(platform, keyguard)

        assertTrue(platform.now() > 0)
        assertNotNull(platform.processID())
        assertFalse(platform.isOwner())
        assertFalse(platform.isSecure())
        assertFalse(platform.isLocked())
        platform.reboot()
        platform.lock()
    }

    @Test
    fun testCaptureAndBundleRoundTrip() {
        val bundle = Bundle().apply {
            putBoolean("key_bool", true)
            putInt("key_int", 42)
            putString("key_str", "hello world")
            putStringArray("key_strings", arrayOf("alpha", "beta"))
            putBundle("key_bundle", Bundle().apply {
                putString("nested_str", "nested value")
            })
            putParcelableArray("key_bundles", arrayOf(
                Bundle().apply { putInt("item_int", 1) },
                Bundle().apply { putInt("item_int", 2) }
            ))
        }

        val captured = AndroidConfigurations.capture(bundle)
        assertNotNull(captured)

        val restored = AndroidConfigurations.bundle(captured)
        assertTrue(restored.getBoolean("key_bool"))
        assertEquals(42, restored.getInt("key_int"))
        assertEquals("hello world", restored.getString("key_str"))
        assertArrayEquals(arrayOf("alpha", "beta"), restored.getStringArray("key_strings"))
        assertEquals("nested value", restored.getBundle("key_bundle")?.getString("nested_str"))

        val array = restored.getParcelableArray("key_bundles")
        assertNotNull(array)
        assertEquals(2, array!!.size)
        assertEquals(1, (array[0] as Bundle).getInt("item_int"))
        assertEquals(2, (array[1] as Bundle).getInt("item_int"))
    }

    @Test
    fun testCaptureDepthLimit() {
        val b5 = Bundle().apply { putString("k5", "v5") }
        val b4 = Bundle().apply { putBundle("k4", b5) }
        val b3 = Bundle().apply { putBundle("k3", b4) }
        val b2 = Bundle().apply { putBundle("k2", b3) }
        val b1 = Bundle().apply { putBundle("k1", b2) }

        assertThrows(PolicyUnsupported::class.java) {
            AndroidConfigurations.capture(b1)
        }
    }

    @Test
    fun testCaptureUnsupportedTypes() {
        val bScalar = Bundle().apply { putDouble("double_val", 3.14) }
        assertThrows(PolicyUnsupported::class.java) { AndroidConfigurations.capture(bScalar) }

        val bArr = Bundle().apply {
            mMap["int_arr"] = arrayOf(1, 2, 3)
        }
        assertThrows(PolicyUnsupported::class.java) { AndroidConfigurations.capture(bArr) }

        val bParcelable = Bundle().apply {
            putParcelableArray("intent_arr", arrayOf(android.content.Intent("TEST")))
        }
        assertThrows(PolicyUnsupported::class.java) { AndroidConfigurations.capture(bParcelable) }
    }

    @Test
    fun testBundleInvalidValueCase() {
        val emptyVal = ConfigurationBundle.newBuilder()
            .addEntries(ConfigurationEntry.newBuilder().setKey("key").setValue(ConfigurationValue.newBuilder()))
            .build()
        assertThrows(IllegalStateException::class.java) {
            AndroidConfigurations.bundle(emptyVal)
        }
    }

    @Test
    fun testConfigurationValuesValidationAndLimits() {
        // Valid bundle normalize, digest, journal and read
        val valid = ConfigurationBundle.newBuilder()
            .addEntries(ConfigurationEntry.newBuilder().setKey("keyA").setValue(ConfigurationValue.newBuilder().setBooleanValue(true)))
            .addEntries(ConfigurationEntry.newBuilder().setKey("keyB").setValue(ConfigurationValue.newBuilder().setStringValue("test")))
            .addEntries(ConfigurationEntry.newBuilder().setKey("keyC").setValue(ConfigurationValue.newBuilder().setStringsValue(
                ConfigurationStrings.newBuilder().addAllValues(listOf("str1", "str2"))
            )))
            .addEntries(ConfigurationEntry.newBuilder().setKey("keyD").setValue(ConfigurationValue.newBuilder().setBundleValue(
                ConfigurationBundle.newBuilder().addEntries(ConfigurationEntry.newBuilder().setKey("nested").setValue(ConfigurationValue.newBuilder().setIntegerValue(10)))
            )))
            .addEntries(ConfigurationEntry.newBuilder().setKey("keyE").setValue(ConfigurationValue.newBuilder().setBundlesValue(
                ConfigurationBundles.newBuilder().addValues(
                    ConfigurationBundle.newBuilder().addEntries(ConfigurationEntry.newBuilder().setKey("item").setValue(ConfigurationValue.newBuilder().setBooleanValue(false)))
                )
            )))
            .build()

        val normalized = ConfigurationValues.normalize(valid)
        assertEquals(5, normalized.entriesCount)
        val digest = ConfigurationValues.digest(valid)
        assertTrue(digest.startsWith("sha256:"))

        val journal = ConfigurationValues.journal(valid)
        val readBack = ConfigurationValues.read(journal)
        assertEquals(normalized, readBack)

        // Invalid key
        val badKey = ConfigurationBundle.newBuilder()
            .addEntries(ConfigurationEntry.newBuilder().setKey("1badKey").setValue(ConfigurationValue.newBuilder().setBooleanValue(true)))
            .build()
        assertThrows(IllegalArgumentException::class.java) { ConfigurationValues.normalize(badKey) }

        // Duplicate key
        val dupKey = ConfigurationBundle.newBuilder()
            .addEntries(ConfigurationEntry.newBuilder().setKey("key").setValue(ConfigurationValue.newBuilder().setBooleanValue(true)))
            .addEntries(ConfigurationEntry.newBuilder().setKey("key").setValue(ConfigurationValue.newBuilder().setBooleanValue(false)))
            .build()
        assertThrows(IllegalArgumentException::class.java) { ConfigurationValues.normalize(dupKey) }

        // Long string (> 4096 bytes)
        val longStr = ConfigurationBundle.newBuilder()
            .addEntries(ConfigurationEntry.newBuilder().setKey("key").setValue(ConfigurationValue.newBuilder().setStringValue("x".repeat(4097))))
            .build()
        assertThrows(IllegalArgumentException::class.java) { ConfigurationValues.normalize(longStr) }

        // Strings list > 64 elements
        val tooManyStrings = ConfigurationBundle.newBuilder()
            .addEntries(ConfigurationEntry.newBuilder().setKey("key").setValue(ConfigurationValue.newBuilder().setStringsValue(
                ConfigurationStrings.newBuilder().addAllValues((1..65).map { "s$it" })
            ))).build()
        assertThrows(IllegalArgumentException::class.java) { ConfigurationValues.normalize(tooManyStrings) }

        // String in strings list > 4096 bytes
        val longStringInList = ConfigurationBundle.newBuilder()
            .addEntries(ConfigurationEntry.newBuilder().setKey("key").setValue(ConfigurationValue.newBuilder().setStringsValue(
                ConfigurationStrings.newBuilder().addValues("x".repeat(4097))
            ))).build()
        assertThrows(IllegalArgumentException::class.java) { ConfigurationValues.normalize(longStringInList) }

        // Bundles list > 64 elements
        val tooManyBundles = ConfigurationBundle.newBuilder()
            .addEntries(ConfigurationEntry.newBuilder().setKey("key").setValue(ConfigurationValue.newBuilder().setBundlesValue(
                ConfigurationBundles.newBuilder().addAllValues((1..65).map { ConfigurationBundle.getDefaultInstance() })
            ))).build()
        assertThrows(IllegalArgumentException::class.java) { ConfigurationValues.normalize(tooManyBundles) }

        // Nested bundle entries > 64
        val nestedBuilder = ConfigurationBundle.newBuilder()
        for (i in 1..65) {
            nestedBuilder.addEntries(ConfigurationEntry.newBuilder().setKey("k$i").setValue(ConfigurationValue.newBuilder().setBooleanValue(true)))
        }
        val tooManyNestedEntries = ConfigurationBundle.newBuilder()
            .addEntries(ConfigurationEntry.newBuilder().setKey("nested").setValue(ConfigurationValue.newBuilder().setBundleValue(nestedBuilder)))
            .build()
        assertThrows(IllegalArgumentException::class.java) { ConfigurationValues.normalize(tooManyNestedEntries) }

        // Depth limit > 4
        var current = ConfigurationBundle.newBuilder().addEntries(ConfigurationEntry.newBuilder().setKey("k").setValue(ConfigurationValue.newBuilder().setBooleanValue(true))).build()
        for (d in 1..5) {
            current = ConfigurationBundle.newBuilder().addEntries(ConfigurationEntry.newBuilder().setKey("nested$d").setValue(ConfigurationValue.newBuilder().setBundleValue(current))).build()
        }
        assertThrows(IllegalArgumentException::class.java) { ConfigurationValues.normalize(current) }

        // Missing valueCase
        val missingVal = ConfigurationBundle.newBuilder()
            .addEntries(ConfigurationEntry.newBuilder().setKey("key").setValue(ConfigurationValue.newBuilder()))
            .build()
        assertThrows(IllegalStateException::class.java) { ConfigurationValues.normalize(missingVal) }
    }
}
