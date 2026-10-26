// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInfo
import android.content.pm.PackageInstaller
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import io.github.thienvu18.kylok.proto.v1.*
import java.io.File
import java.io.IOException
import java.security.MessageDigest

internal class AndroidInstallPlatform(private val context: Context, private val storage: StateAccess) : InstallPlatform {
    private val pm = context.packageManager
    private val installer = pm.packageInstaller
    private val staging = File(context.noBackupFilesDir, "kylok-apks").apply { mkdirs() }
    /** Choose an operation-specific APK inside the private no-backup staging directory. */
    private fun file(op: Operation) = File(staging, "${op.id}.apk")
    /** Read wall time for operation deadline checks. */
    override fun now() = System.currentTimeMillis()
    /** Check effective Device Owner before installation. */
    override fun isOwner() = Kylok.owner(context)
    /** Canonicalize current APK signers as sorted SHA-256 certificate digests. */
    private fun certificates(info: PackageInfo): List<String> = info.signingInfo?.apkContentsSigners?.map {
        MessageDigest.getInstance("SHA-256").digest(it.toByteArray()).joinToString("") { b -> "%02x".format(b) }
    }?.sorted().orEmpty()

    /** Read installed version and current signers or report package absence. */
    override fun installed(packageName: String): InstalledApplication? {
        val info = try { pm.getPackageInfo(packageName, PackageManager.PackageInfoFlags.of(PackageManager.GET_SIGNING_CERTIFICATES.toLong())) }
        catch (absent: PackageManager.NameNotFoundException) { return null }
        return InstalledApplication.newBuilder().setPackageName(info.packageName).setVersionCode(info.longVersionCode)
            .addAllSigningCertificates(certificates(info)).build()
    }

    /** Reserve staging/session headroom before downloading against the enrolled origin. */
    override fun download(op: Operation, progress: (Long) -> Unit) {
        val state = storage.read()
        val manager = context.getSystemService(android.os.storage.StorageManager::class.java)
        val available = manager.getAllocatableBytes(manager.getUuidForPath(staging))
        // Reserve remaining download bytes, a full installer copy, and 8 MiB for session/metadata overhead.
        if (available < maxOf(0L, op.artifact.sizeBytes - file(op).length()) + op.artifact.sizeBytes + 8 * 1024 * 1024) throw InstallRejected("INSUFFICIENT_STORAGE")
        ArtifactDownload.fetch(state.getString("server"), state.getString("credential"), state.getString("device_id"), op, file(op), progress)
    }

    /** Recheck staged content, platform requirements, metadata and permitted signing lineage. */
    override fun verify(op: Operation) {
        val apk = op.artifact
        if (Build.VERSION.SDK_INT < apk.minSdk || apk.maxSdk != 0 && Build.VERSION.SDK_INT > apk.maxSdk) throw InstallRejected("INCOMPATIBLE_SDK")
        if (apk.supportedAbisCount > 0 && apk.supportedAbisList.none { it in Build.SUPPORTED_ABIS }) throw InstallRejected("INCOMPATIBLE_ABI")
        // Recheck immutable bytes even when recovering a previously staged session.
        ArtifactDownload.fetch(storage.read().getString("server"), storage.read().getString("credential"),
            storage.read().getString("device_id"), op, file(op)) { }
        val info = pm.getPackageArchiveInfo(file(op).path, PackageManager.PackageInfoFlags.of(
            (PackageManager.GET_SIGNING_CERTIFICATES or PackageManager.GET_CONFIGURATIONS).toLong())) ?: throw InstallRejected("APK_PARSE_FAILED")
        if (info.packageName != apk.packageName || info.longVersionCode != apk.versionCode || certificates(info) != apk.signingCertificatesList.sorted()) throw InstallRejected("APK_METADATA_MISMATCH")
        if (!info.splitNames.isNullOrEmpty()) throw InstallRejected("SPLIT_APK_UNSUPPORTED")
        info.reqFeatures?.filter { it.flags and android.content.pm.FeatureInfo.FLAG_REQUIRED != 0 }?.forEach {
            if (it.name != null && !pm.hasSystemFeature(it.name, it.version)) throw InstallRejected("MISSING_REQUIRED_FEATURE")
            if (it.reqGlEsVersion != 0) {
                val activity = context.getSystemService(android.app.ActivityManager::class.java)
                if (activity.deviceConfigurationInfo.reqGlEsVersion < it.reqGlEsVersion) throw InstallRejected("INCOMPATIBLE_OPENGL")
            }
        }
        val installed = installed(apk.packageName)
        if (installed != null) {
            if (installed.versionCode > apk.versionCode) throw InstallRejected("DOWNGRADE_REJECTED")
            val signing = info.signingInfo
            val history = signing?.signingCertificateHistory?.map { cert ->
                MessageDigest.getInstance("SHA-256").digest(cert.toByteArray()).joinToString("") { "%02x".format(it) }
            }.orEmpty()
            val compatible = installed.signingCertificatesList.sorted() == certificates(info) ||
                installed.signingCertificatesCount == 1 && signing?.hasMultipleSigners() == false && installed.signingCertificatesList.single() in history
            if (!compatible) throw InstallRejected("SIGNING_IDENTITY_MISMATCH")
        }
    }

    /** Create a full-install session marked as Kylok-owned for recovery and cleanup. */
    override fun createSession(op: Operation): Int {
        val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL).apply {
            setAppPackageName(op.artifact.packageName)
            setOriginatingUri(Uri.parse("kylok://install/${op.id}"))
            setSize(op.artifact.sizeBytes)
            setRequireUserAction(PackageInstaller.SessionParams.USER_ACTION_NOT_REQUIRED)
        }
        return installer.createSession(params)
    }
    /** Copy and fsync the verified APK into the journaled session. */
    override fun writeSession(id: Int, op: Operation) {
        installer.openSession(id).use { session ->
            session.openWrite("base.apk", 0, op.artifact.sizeBytes).use { out ->
                file(op).inputStream().use { it.copyTo(out, 64 * 1024) }
                session.fsync(out)
            }
        }
    }
    /** Observe whether the OS still holds the journaled session. */
    override fun sessionExists(id: Int) = installer.getSessionInfo(id) != null
    /** Detect a previously committed session to suppress duplicate commits. */
    override fun sessionSealed(id: Int) = installer.getSessionInfo(id)?.isSealed == true
    /** Bind the OS result receiver to the operation/session and commit the existing session. */
    override fun commit(id: Int, operationID: String) {
        val intent = Intent(context, InstallResultReceiver::class.java).setAction("io.github.thienvu18.kylok.INSTALL_RESULT")
            .setData(Uri.parse("kylok://install/$operationID/$id")).putExtra("operation_id", operationID).putExtra("session_id", id)
        // PackageInstaller must fill the result extras; target 35+ rejects immutable status receivers.
        val sender = PendingIntent.getBroadcast(context, id, intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_MUTABLE).intentSender
        installer.openSession(id).use { it.commit(sender) }
    }
    /** Best-effort release an obsolete installer session. */
    override fun abandon(id: Int) { runCatching { installer.abandonSession(id) } }
    /** Delete obsolete staging files and only unsealed sessions with Kylok ownership markers. */
    override fun cleanup(retained: Set<String>, sessions: Set<Int>) {
        staging.listFiles()?.forEach { file -> if (file.name.removeSuffix(".apk") !in retained) file.delete() }
        installer.mySessions.filter { it.originatingUri?.scheme == "kylok" && it.originatingUri?.host == "install" &&
            it.sessionId !in sessions && !it.isSealed }.forEach { abandon(it.sessionId) }
    }
}
