// SPDX-License-Identifier: LGPL-3.0-only
package android.content.pm

import android.content.IntentSender
import android.net.Uri
import java.io.ByteArrayOutputStream
import java.io.OutputStream

/** Test double for PackageInstaller.SessionInfo. */
class TestPackageInstallerSessionInfo(
    private val sId: Int,
    private val origUri: Uri? = null,
    private val sealed: Boolean = false
) : PackageInstaller.SessionInfo() {
    override fun getSessionId(): Int = sId
    override fun getOriginatingUri(): Uri? = origUri
    override fun isSealed(): Boolean = sealed
}

/** Test double for PackageInstaller.Session. */
class TestPackageInstallerSession(private val sId: Int) : PackageInstaller.Session() {
    var committed: Boolean = false
    var abandoned: Boolean = false
    val writtenBytes = ByteArrayOutputStream()

    override fun openWrite(name: String, offsetBytes: Long, lengthBytes: Long): OutputStream = writtenBytes
    override fun fsync(out: OutputStream) {}
    override fun commit(statusReceiver: IntentSender) { committed = true }
    override fun abandon() { abandoned = true }
    override fun close() {}
}

/** Test double for PackageInstaller. */
class TestPackageInstaller : PackageInstaller() {
    val sessions = mutableMapOf<Int, TestPackageInstallerSession>()
    val sessionInfos = mutableMapOf<Int, TestPackageInstallerSessionInfo>()
    var nextSessionId = 1
    val abandonedSessions = mutableListOf<Int>()

    override fun createSession(params: SessionParams): Int {
        val id = nextSessionId++
        val uri = try {
            val field = params.javaClass.getDeclaredField("originatingUri")
            field.isAccessible = true
            field.get(params) as? Uri
        } catch (_: Exception) {
            Uri.parse("kylok://install/test/$id")
        }
        sessionInfos[id] = TestPackageInstallerSessionInfo(id, uri, false)
        sessions[id] = TestPackageInstallerSession(id)
        return id
    }

    override fun openSession(sessionId: Int): PackageInstaller.Session =
        sessions[sessionId] ?: TestPackageInstallerSession(sessionId).also { sessions[sessionId] = it }

    override fun getSessionInfo(sessionId: Int): PackageInstaller.SessionInfo? =
        sessionInfos[sessionId]

    override fun getMySessions(): List<PackageInstaller.SessionInfo> =
        sessionInfos.values.toList()

    override fun abandonSession(sessionId: Int) {
        abandonedSessions.add(sessionId)
        sessions[sessionId]?.abandon()
        sessionInfos.remove(sessionId)
    }
}
