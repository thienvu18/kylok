// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import com.sun.net.httpserver.HttpServer
import io.github.thienvu18.kylok.proto.v1.*
import org.junit.Assert.*
import org.junit.Test
import java.io.File
import java.net.InetSocketAddress
import java.nio.file.Files
import java.security.MessageDigest

class ArtifactDownloadTest {
    @Test fun relativeDownloadUsesTheEnrolledOrigin() = fixture { origin, op, bytes, file, server ->
        val relative = op.toBuilder().setDownloadUrl(java.net.URI(op.downloadUrl).rawPath).build()
        server.createContext("/artifacts/") { exchange ->
            assertEquals("Bearer credential", exchange.requestHeaders.getFirst("Authorization"))
            exchange.responseHeaders.set("ETag", "\"${op.artifact.sha256}\"")
            exchange.sendResponseHeaders(200, bytes.size.toLong())
            exchange.responseBody.use { it.write(bytes) }
        }
        server.start()
        ArtifactDownload.fetch(origin, "credential", "device", relative, file) { }
        assertArrayEquals(bytes, file.readBytes())
    }
    @Test fun networkPathAndSchemeChangesAreRejectedBeforeSendingCredentials() = fixture { origin, op, _, file, _ ->
        for (url in listOf("//example.invalid/artifacts/${op.id}/${op.artifact.sha256}.apk",
            "https://127.0.0.1/artifacts/${op.id}/${op.artifact.sha256}.apk", "${op.downloadUrl}?unexpected=1")) {
            try { ArtifactDownload.fetch(origin, "credential", "device", op.toBuilder().setDownloadUrl(url).build(), file) { }; fail("unsafe URL accepted: $url") }
            catch (_: IllegalArgumentException) { }
        }
    }
    private fun fixture(test: (String, Operation, ByteArray, File, HttpServer) -> Unit) {
        val bytes = ByteArray(200_000) { (it % 251).toByte() }
        val digest = MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        val origin = "http://127.0.0.1:${server.address.port}"
        val operation = Operation.newBuilder().setId("a".repeat(43)).setLeaseId("b".repeat(48))
            .setArtifact(Artifact.newBuilder().setSha256(digest).setSizeBytes(bytes.size.toLong()))
            .setDownloadUrl("$origin/artifacts/${"a".repeat(43)}/$digest.apk").build()
        val directory = Files.createTempDirectory("kylok-download").toFile()
        try { test(origin, operation, bytes, File(directory, "test.apk"), server) }
        finally { server.stop(0); directory.deleteRecursively() }
    }
    @Test fun resumesFromExistingPartialFileAndVerifiesDigest() = fixture { origin, op, bytes, file, server ->
        file.writeBytes(bytes.copyOfRange(0, 1000))
        server.createContext("/") { exchange ->
            assertEquals("bytes=1000-", exchange.requestHeaders.getFirst("Range"))
            assertEquals("Bearer credential", exchange.requestHeaders.getFirst("Authorization"))
            assertEquals(op.leaseId, exchange.requestHeaders.getFirst("X-Kylok-Lease-ID"))
            exchange.responseHeaders.set("ETag", "\"${op.artifact.sha256}\"")
            exchange.responseHeaders.set("Content-Range", "bytes 1000-${bytes.size - 1}/${bytes.size}")
            exchange.sendResponseHeaders(206, (bytes.size - 1000).toLong())
            exchange.responseBody.use { it.write(bytes, 1000, bytes.size - 1000) }
        }
        server.start()
        var progress = 0L
        ArtifactDownload.fetch(origin, "credential", "device", op, file) { progress = it }
        assertArrayEquals(bytes, file.readBytes()); assertEquals(bytes.size.toLong(), progress)
    }
    @Test fun serverIgnoringRangeRestartsDownload() = fixture { origin, op, bytes, file, server ->
        file.writeBytes(bytes.copyOfRange(0, 1000))
        server.createContext("/") { exchange ->
            exchange.responseHeaders.set("ETag", "\"${op.artifact.sha256}\"")
            exchange.sendResponseHeaders(200, bytes.size.toLong()); exchange.responseBody.use { it.write(bytes) }
        }
        server.start(); ArtifactDownload.fetch(origin, "credential", "device", op, file) { }
        assertArrayEquals(bytes, file.readBytes())
    }
    @Test fun corruptCompleteFileIsRejectedAndDeleted() = fixture { origin, op, bytes, file, _ ->
        file.writeBytes(ByteArray(bytes.size))
        try { ArtifactDownload.fetch(origin, "credential", "device", op, file) { }; fail("corruption accepted") }
        catch (error: InstallRejected) { assertEquals("APK_DIGEST_MISMATCH", error.reason) }
        assertFalse(file.exists())
    }
    @Test fun invalidRangeAndRedirectCannotLeakCredentials() = fixture { origin, op, bytes, file, server ->
        file.writeBytes(bytes.copyOfRange(0, 1000))
        server.createContext("/") { exchange ->
            exchange.responseHeaders.set("ETag", "\"${op.artifact.sha256}\"")
            exchange.responseHeaders.set("Content-Range", "bytes 0-100/100")
            exchange.sendResponseHeaders(206, 1); exchange.responseBody.use { it.write(1) }
        }
        server.start()
        try { ArtifactDownload.fetch(origin, "credential", "device", op, file) { }; fail("range accepted") }
        catch (error: InstallRejected) { assertEquals("INVALID_CONTENT_RANGE", error.reason) }
        val outside = op.toBuilder().setDownloadUrl("http://example.invalid/artifact.apk").build()
        try { ArtifactDownload.fetch(origin, "credential", "device", outside, file) { }; fail("cross-origin accepted") }
        catch (_: IllegalArgumentException) { }
    }
    @Test fun truncatedResponseKeepsPartialBytesForNextRangeRequest() = fixture { origin, op, bytes, file, server ->
        var calls = 0
        server.createContext("/") { exchange ->
            calls++
            exchange.responseHeaders.set("ETag", "\"${op.artifact.sha256}\"")
            if (calls == 1) {
                exchange.sendResponseHeaders(200, 1000)
                exchange.responseBody.use { it.write(bytes, 0, 1000) }
            } else {
                assertEquals("bytes=1000-", exchange.requestHeaders.getFirst("Range"))
                exchange.responseHeaders.set("Content-Range", "bytes 1000-${bytes.size - 1}/${bytes.size}")
                exchange.sendResponseHeaders(206, (bytes.size - 1000).toLong())
                exchange.responseBody.use { it.write(bytes, 1000, bytes.size - 1000) }
            }
        }
        server.start()
        try { ArtifactDownload.fetch(origin, "credential", "device", op, file) { }; fail("short body accepted") }
        catch (_: java.io.IOException) { }
        assertEquals(1000L, file.length())
        ArtifactDownload.fetch(origin, "credential", "device", op, file) { }
        assertArrayEquals(bytes, file.readBytes()); assertEquals(2, calls)
    }
    @Test fun redirectIsNotFollowedAndChangedEtagIsRejected() = fixture { origin, op, _, file, server ->
        var followed = false; var redirect = true
        server.createContext("/outside") { exchange -> followed = true; exchange.sendResponseHeaders(403, -1); exchange.close() }
        server.createContext("/artifacts/") { exchange ->
            if (redirect) { exchange.responseHeaders.set("Location", "$origin/outside"); exchange.sendResponseHeaders(302, -1) }
            else { exchange.responseHeaders.set("ETag", "wrong"); exchange.sendResponseHeaders(200, -1) }
            exchange.close()
        }
        server.start()
        try { ArtifactDownload.fetch(origin, "credential", "device", op, file) { }; fail("redirect accepted") }
        catch (_: java.io.IOException) { }
        assertFalse(followed)
        redirect = false
        try { ArtifactDownload.fetch(origin, "credential", "device", op, file) { }; fail("content change accepted") }
        catch (error: InstallRejected) { assertEquals("ARTIFACT_CHANGED", error.reason) }
    }
}
