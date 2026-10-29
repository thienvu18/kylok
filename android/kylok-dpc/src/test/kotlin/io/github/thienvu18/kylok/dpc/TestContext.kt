// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.content.*
import android.content.pm.*
import android.content.res.AssetManager
import android.content.res.Configuration
import android.content.res.Resources
import android.database.sqlite.SQLiteDatabase
import android.graphics.Bitmap
import android.graphics.drawable.Drawable
import android.net.Uri
import android.os.*
import android.view.Display
import java.io.File
import java.io.FileInputStream
import java.io.FileOutputStream
import java.io.InputStream

/** Shared test context implementing Android platform interfaces for deterministic unit testing. */
@Suppress("DEPRECATION")
internal open class TestContext(
    /** Base temporary directory for file and directory operations. */
    val rootDir: File = File(System.getProperty("java.io.tmpdir"), "test-context-" + System.nanoTime()).apply { mkdirs() }
) : Context() {

    val services = mutableMapOf<String, Any>()
    val startedActivities = mutableListOf<Intent>()
    val sentBroadcasts = mutableListOf<Intent>()
    var customPackageManager: PackageManager? = null

    /** Register a system service for lookup by class or string identifier. */
    fun <T : Any> registerService(serviceClass: Class<T>, instance: T) {
        services[serviceClass.name] = instance
        val serviceName = getSystemServiceName(serviceClass)
        if (serviceName != null) {
            services[serviceName] = instance
        }
    }

    override fun getPackageName(): String = "io.github.thienvu18.kylok.dpc"
    var forceNullAppContext: Boolean = false
    override fun getApplicationContext(): Context? = if (forceNullAppContext) null else this
    override fun getFilesDir(): File = File(rootDir, "files").apply { mkdirs() }
    override fun getNoBackupFilesDir(): File = File(rootDir, "no_backup").apply { mkdirs() }
    override fun getDataDir(): File = rootDir
    override fun getCacheDir(): File = File(rootDir, "cache").apply { mkdirs() }
    override fun getCodeCacheDir(): File = File(rootDir, "code_cache").apply { mkdirs() }
    override fun getDatabasePath(name: String): File = File(File(rootDir, "databases").apply { mkdirs() }, name)

    override fun getSystemServiceName(serviceClass: Class<*>): String? = when (serviceClass) {
        android.app.admin.DevicePolicyManager::class.java -> Context.DEVICE_POLICY_SERVICE
        android.app.KeyguardManager::class.java -> Context.KEYGUARD_SERVICE
        android.app.ActivityManager::class.java -> Context.ACTIVITY_SERVICE
        android.os.UserManager::class.java -> Context.USER_SERVICE
        android.net.wifi.WifiManager::class.java -> Context.WIFI_SERVICE
        android.net.ConnectivityManager::class.java -> Context.CONNECTIVITY_SERVICE
        android.os.storage.StorageManager::class.java -> Context.STORAGE_SERVICE
        android.app.NotificationManager::class.java -> Context.NOTIFICATION_SERVICE
        else -> serviceClass.name
    }
    override fun getSystemService(name: String): Any? = services[name]

    val defaultPackageManager = TestPackageManager()
    override fun getPackageManager(): PackageManager = customPackageManager ?: defaultPackageManager

    val permissionGrants = mutableMapOf<String, Int>()
    override fun checkSelfPermission(permission: String): Int =
        permissionGrants[permission]
            ?: defaultPackageManager.permissionGrants[permission]
            ?: PackageManager.PERMISSION_GRANTED
    override fun startActivity(intent: Intent?) { intent?.let { startedActivities.add(it) } }
    override fun startActivity(intent: Intent?, options: Bundle?) { intent?.let { startedActivities.add(it) } }
    override fun sendBroadcast(intent: Intent?) { intent?.let { sentBroadcasts.add(it) } }
    override fun sendBroadcast(intent: Intent?, receiverPermission: String?) { intent?.let { sentBroadcasts.add(it) } }

    override fun getAssets(): AssetManager = throw UnsupportedOperationException()
    val resourcesInstance = android.content.res.TestResources()
    override fun getResources(): Resources = resourcesInstance
    override fun getContentResolver(): ContentResolver = object : ContentResolver(this) {}
    override fun getMainLooper(): Looper = Looper.getMainLooper()
    override fun setTheme(resid: Int) {}
    override fun getTheme(): Resources.Theme = throw UnsupportedOperationException()
    override fun getClassLoader(): ClassLoader = javaClass.classLoader
    override fun getApplicationInfo(): ApplicationInfo = ApplicationInfo().apply {
        packageName = "io.github.thienvu18.kylok.dpc"
        dataDir = rootDir.absolutePath
    }
    override fun getPackageResourcePath(): String = rootDir.absolutePath
    override fun getPackageCodePath(): String = rootDir.absolutePath
    override fun getSharedPreferences(name: String, mode: Int): SharedPreferences = throw UnsupportedOperationException()
    override fun moveSharedPreferencesFrom(sourceContext: Context, name: String): Boolean = false
    override fun deleteSharedPreferences(name: String): Boolean = false
    override fun openFileInput(name: String): FileInputStream = FileInputStream(File(filesDir, name))
    override fun openFileOutput(name: String, mode: Int): FileOutputStream = FileOutputStream(File(filesDir, name))
    override fun deleteFile(name: String): Boolean = File(filesDir, name).delete()
    override fun getFileStreamPath(name: String): File = File(filesDir, name)
    override fun getExternalFilesDir(type: String?): File = File(rootDir, "external_files").apply { mkdirs() }
    override fun getExternalFilesDirs(type: String?): Array<File> = arrayOf(getExternalFilesDir(type))
    override fun getObbDir(): File = File(rootDir, "obb").apply { mkdirs() }
    override fun getObbDirs(): Array<File> = arrayOf(getObbDir())
    override fun getExternalCacheDir(): File = File(rootDir, "external_cache").apply { mkdirs() }
    override fun getExternalCacheDirs(): Array<File> = arrayOf(getExternalCacheDir())
    override fun getExternalMediaDirs(): Array<File> = arrayOf(File(rootDir, "media").apply { mkdirs() })
    override fun fileList(): Array<String> = filesDir.list() ?: emptyArray()
    override fun getDir(name: String, mode: Int): File = File(rootDir, name).apply { mkdirs() }
    override fun openOrCreateDatabase(name: String, mode: Int, factory: SQLiteDatabase.CursorFactory?): SQLiteDatabase =
        throw UnsupportedOperationException()
    override fun openOrCreateDatabase(name: String, mode: Int, factory: SQLiteDatabase.CursorFactory?, errorHandler: android.database.DatabaseErrorHandler?): SQLiteDatabase =
        throw UnsupportedOperationException()
    override fun moveDatabaseFrom(sourceContext: Context, name: String): Boolean = false
    override fun deleteDatabase(name: String): Boolean = getDatabasePath(name).delete()
    override fun databaseList(): Array<String> = File(rootDir, "databases").list() ?: emptyArray()
    override fun getWallpaper(): Drawable = throw UnsupportedOperationException()
    override fun peekWallpaper(): Drawable = throw UnsupportedOperationException()
    override fun getWallpaperDesiredMinimumWidth(): Int = 0
    override fun getWallpaperDesiredMinimumHeight(): Int = 0
    override fun setWallpaper(bitmap: Bitmap?) {}
    override fun setWallpaper(data: InputStream?) {}
    override fun clearWallpaper() {}
    override fun startActivities(intents: Array<out Intent>?) {}
    override fun startActivities(intents: Array<out Intent>?, options: Bundle?) {}
    override fun startIntentSender(intent: IntentSender?, fillInIntent: Intent?, flagsMask: Int, flagsValues: Int, extraFlags: Int) {}
    override fun startIntentSender(intent: IntentSender?, fillInIntent: Intent?, flagsMask: Int, flagsValues: Int, extraFlags: Int, options: Bundle?) {}
    override fun sendOrderedBroadcast(intent: Intent?, receiverPermission: String?) {}
    override fun sendOrderedBroadcast(intent: Intent, receiverPermission: String?, resultReceiver: BroadcastReceiver?, scheduler: Handler?, initialCode: Int, initialData: String?, initialExtras: Bundle?) {}
    override fun sendBroadcastAsUser(intent: Intent?, user: UserHandle?) {}
    override fun sendBroadcastAsUser(intent: Intent?, user: UserHandle?, receiverPermission: String?) {}
    override fun sendOrderedBroadcastAsUser(intent: Intent?, user: UserHandle?, receiverPermission: String?, resultReceiver: BroadcastReceiver?, scheduler: Handler?, initialCode: Int, initialData: String?, initialExtras: Bundle?) {}
    override fun sendStickyBroadcast(intent: Intent?) {}
    override fun sendStickyOrderedBroadcast(intent: Intent?, resultReceiver: BroadcastReceiver?, scheduler: Handler?, initialCode: Int, initialData: String?, initialExtras: Bundle?) {}
    override fun removeStickyBroadcast(intent: Intent?) {}
    override fun sendStickyBroadcastAsUser(intent: Intent?, user: UserHandle?) {}
    override fun sendStickyOrderedBroadcastAsUser(intent: Intent?, user: UserHandle?, resultReceiver: BroadcastReceiver?, scheduler: Handler?, initialCode: Int, initialData: String?, initialExtras: Bundle?) {}
    override fun removeStickyBroadcastAsUser(intent: Intent?, user: UserHandle?) {}
    override fun registerReceiver(receiver: BroadcastReceiver?, filter: IntentFilter?): Intent? = null
    override fun registerReceiver(receiver: BroadcastReceiver?, filter: IntentFilter?, flags: Int): Intent? = null
    override fun registerReceiver(receiver: BroadcastReceiver?, filter: IntentFilter?, broadcastPermission: String?, scheduler: Handler?): Intent? = null
    override fun registerReceiver(receiver: BroadcastReceiver?, filter: IntentFilter?, broadcastPermission: String?, scheduler: Handler?, flags: Int): Intent? = null
    override fun unregisterReceiver(receiver: BroadcastReceiver?) {}
    override fun startService(service: Intent?): ComponentName? = null
    override fun startForegroundService(service: Intent?): ComponentName? = null
    override fun stopService(service: Intent?): Boolean = false
    override fun bindService(service: Intent, conn: ServiceConnection, flags: Int): Boolean = false
    override fun unbindService(conn: ServiceConnection) {}
    override fun startInstrumentation(className: ComponentName, profileFile: String?, arguments: Bundle?): Boolean = false
    override fun checkPermission(permission: String, pid: Int, uid: Int): Int = PackageManager.PERMISSION_GRANTED
    override fun checkCallingPermission(permission: String): Int = PackageManager.PERMISSION_GRANTED
    override fun checkCallingOrSelfPermission(permission: String): Int = PackageManager.PERMISSION_GRANTED
    override fun enforcePermission(permission: String, pid: Int, uid: Int, message: String?) {}
    override fun enforceCallingPermission(permission: String, message: String?) {}
    override fun enforceCallingOrSelfPermission(permission: String, message: String?) {}
    override fun grantUriPermission(toPackage: String?, uri: Uri?, modeFlags: Int) {}
    override fun revokeUriPermission(uri: Uri?, modeFlags: Int) {}
    override fun revokeUriPermission(toPackage: String?, uri: Uri?, modeFlags: Int) {}
    override fun checkUriPermission(uri: Uri?, pid: Int, uid: Int, modeFlags: Int): Int = PackageManager.PERMISSION_GRANTED
    override fun checkCallingUriPermission(uri: Uri?, modeFlags: Int): Int = PackageManager.PERMISSION_GRANTED
    override fun checkCallingOrSelfUriPermission(uri: Uri?, modeFlags: Int): Int = PackageManager.PERMISSION_GRANTED
    override fun checkUriPermission(uri: Uri?, readPermission: String?, writePermission: String?, pid: Int, uid: Int, modeFlags: Int): Int = PackageManager.PERMISSION_GRANTED
    override fun enforceUriPermission(uri: Uri?, pid: Int, uid: Int, modeFlags: Int, message: String?) {}
    override fun enforceUriPermission(uri: Uri?, readPermission: String?, writePermission: String?, pid: Int, uid: Int, modeFlags: Int, message: String?) {}
    override fun enforceCallingUriPermission(uri: Uri?, modeFlags: Int, message: String?) {}
    override fun enforceCallingOrSelfUriPermission(uri: Uri?, modeFlags: Int, message: String?) {}
    override fun createPackageContext(packageName: String?, flags: Int): Context? = null
    override fun createContextForSplit(splitName: String?): Context? = null
    override fun createConfigurationContext(overrideConfiguration: Configuration): Context? = null
    override fun createDisplayContext(display: Display): Context? = null
    override fun isRestricted(): Boolean = false
    override fun createDeviceProtectedStorageContext(): Context = this
    override fun isDeviceProtectedStorage(): Boolean = false
}


