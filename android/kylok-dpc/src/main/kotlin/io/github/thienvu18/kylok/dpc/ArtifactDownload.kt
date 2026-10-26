// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import io.github.thienvu18.kylok.proto.v1.Operation
import java.io.File
import java.io.IOException
import java.io.RandomAccessFile
import java.net.HttpURLConnection
import java.net.URI
import java.security.MessageDigest

/** Uses bounded range requests, a fixed origin, verified bytes, and fsynced checkpoints. */
internal object ArtifactDownload {
    /** Resume authorized same-origin bytes, fsync progress and verify immutable size and digest. */
    fun fetch(origin: String, credential: String, deviceID: String, op: Operation, file: File, progress: (Long) -> Unit) {
        val server = URI(origin)
        // Resolve server paths against the enrolled origin; the checks below
        // still reject absolute URLs that would move credentials to another host.
        val uri = server.resolve(URI(op.downloadUrl))
        require(uri.scheme == server.scheme && uri.rawAuthority == server.rawAuthority && uri.userInfo == null &&
            uri.rawQuery == null && uri.rawFragment == null && uri.rawPath == "/artifacts/${op.id}/${op.artifact.sha256}.apk")
        val expected = op.artifact.sizeBytes
        if (expected !in 1..(64L * 1024 * 1024)) throw InstallRejected("APK_SIZE_INVALID")
        if (file.length() > expected) file.delete()
        if (file.length() != expected) {
            file.parentFile!!.mkdirs()
            val offset = file.length()
            val connection = uri.toURL().openConnection() as HttpURLConnection
            try {
                connection.instanceFollowRedirects = false
                connection.connectTimeout = 10_000; connection.readTimeout = 20_000
                connection.setRequestProperty("Authorization", "Bearer $credential")
                connection.setRequestProperty("X-Kylok-Device-ID", deviceID)
                connection.setRequestProperty("X-Kylok-Lease-ID", op.leaseId)
                connection.setRequestProperty("Accept-Encoding", "identity")
                if (offset > 0) {
                    connection.setRequestProperty("Range", "bytes=$offset-")
                    connection.setRequestProperty("If-Range", "\"${op.artifact.sha256}\"")
                }
                val code = connection.responseCode
                if (code == 401 || code == 403) throw IOException("Download authorization must be refreshed by sync")
                if (code != 200 && code != 206) throw IOException("APK download HTTP $code")
                if (code == 206 && (offset == 0L || connection.getHeaderField("Content-Range") != "bytes $offset-${expected - 1}/$expected")) throw InstallRejected("INVALID_CONTENT_RANGE")
                if (connection.getHeaderField("ETag") != "\"${op.artifact.sha256}\"") throw InstallRejected("ARTIFACT_CHANGED")
                RandomAccessFile(file, "rw").use { output ->
                    var total = if (code == 206) offset else 0L
                    output.setLength(total); output.seek(total)
                    var checkpoint = total
                    connection.inputStream.use { input ->
                        val buffer = ByteArray(64 * 1024)
                        while (true) {
                            if (Thread.currentThread().isInterrupted) throw IOException("Cancelled")
                            val count = input.read(buffer); if (count < 0) break
                            if (total + count > expected) throw InstallRejected("APK_SIZE_MISMATCH")
                            output.write(buffer, 0, count); total += count
                            if (total - checkpoint >= 1024 * 1024) { output.fd.sync(); progress(total); checkpoint = total }
                        }
                    }
                    output.fd.sync(); progress(total)
                    if (total != expected) throw IOException("Incomplete APK download")
                }
            } finally { connection.disconnect() }
        }
        val digest = MessageDigest.getInstance("SHA-256")
        file.inputStream().use { input -> val buffer = ByteArray(64 * 1024); while (true) { val n = input.read(buffer); if (n < 0) break; digest.update(buffer, 0, n) } }
        if (digest.digest().joinToString("") { "%02x".format(it) } != op.artifact.sha256) {
            file.delete(); throw InstallRejected("APK_DIGEST_MISMATCH")
        }
        progress(expected)
    }
}
