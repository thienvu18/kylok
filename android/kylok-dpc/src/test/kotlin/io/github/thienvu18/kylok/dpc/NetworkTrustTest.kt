// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import com.google.protobuf.ByteString
import com.sun.net.httpserver.HttpsConfigurator
import com.sun.net.httpserver.HttpsServer
import io.github.thienvu18.kylok.proto.v1.CACertificate
import io.github.thienvu18.kylok.proto.v1.DesiredNetworkPolicy
import io.github.thienvu18.kylok.proto.v1.NetworkPolicy
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.net.InetSocketAddress
import java.net.URL
import java.nio.file.Files
import java.security.KeyStore
import javax.net.ssl.*

class NetworkTrustTest {
    @Test fun privateTrustRotationKeepsHostnameVerificationAndDoesNotChangeGlobalTrust() {
        val directory = Files.createTempDirectory("kylok-network-tls-").toFile()
        val storeFile = directory.resolve("server.p12")
        val password = "fixture-only-password".toCharArray()
        val tool = java.io.File(System.getProperty("java.home"), "bin/keytool")
        val process = ProcessBuilder(tool.path,"-genkeypair","-alias","server","-keyalg","EC","-groupname","secp256r1",
            "-keystore",storeFile.path,"-storetype","PKCS12","-storepass",String(password),"-dname","CN=localhost","-validity","2",
            "-ext","bc=ca:true","-ext","ku=digitalSignature,keyCertSign,cRLSign","-ext","san=dns:localhost","-noprompt")
            .redirectErrorStream(true).start()
        val output = process.inputStream.bufferedReader().readText()
        assertEquals(output,0,process.waitFor())
        val keys = KeyStore.getInstance("PKCS12").apply { storeFile.inputStream().use { load(it,password) } }
        val certificate = keys.getCertificate("server").encoded
        val managers = KeyManagerFactory.getInstance(KeyManagerFactory.getDefaultAlgorithm()).apply { init(keys,password) }
        val ssl = SSLContext.getInstance("TLS").apply { init(managers.keyManagers,null,null) }
        val server = HttpsServer.create(InetSocketAddress("127.0.0.1",0),0).apply {
            httpsConfigurator = HttpsConfigurator(ssl)
            createContext("/") { exchange -> exchange.sendResponseHeaders(204,-1); exchange.close() }
            start()
        }
        val originalTrust = HttpsURLConnection.getDefaultSSLSocketFactory()
        val originalVerifier = HttpsURLConnection.getDefaultHostnameVerifier()
        val state = JSONObject().put("network_cas",JSONObject().put(caDigest(certificate),JSONObject().put("baseline",false).put("trust_baseline",false)))
        val proposed = NetworkPolicy.newBuilder().addCertificates(CACertificate.newBuilder().setId("root").setDer(ByteString.copyFrom(certificate))).build()
        val propertyNames=listOf("javax.net.ssl.trustStore","javax.net.ssl.trustStorePassword","javax.net.ssl.trustStoreType")
        val originalProperties=propertyNames.associateWith { System.getProperty(it) }
        try {
            val factory = NetworkTrust.factory(state,proposed)!!
            val accepted = URL("https://localhost:${server.address.port}/").openConnection() as HttpsURLConnection
            accepted.sslSocketFactory = factory; accepted.connectTimeout=2000; accepted.readTimeout=2000
            try { assertEquals(204,accepted.responseCode) } finally { accepted.disconnect() }
            val wrongHost = URL("https://127.0.0.1:${server.address.port}/").openConnection() as HttpsURLConnection
            wrongHost.sslSocketFactory=factory;wrongHost.connectTimeout=2000;wrongHost.readTimeout=2000
            try { wrongHost.responseCode;fail("private CA bypassed hostname verification") } catch (_: SSLException) { }
            finally { wrongHost.disconnect() }
            val removed = URL("https://localhost:${server.address.port}/").openConnection() as HttpsURLConnection
            removed.sslSocketFactory=NetworkTrust.factory(state,NetworkPolicy.getDefaultInstance())!!;removed.connectTimeout=2000;removed.readTimeout=2000
            try { removed.responseCode;fail("removed private root still trusted") } catch (_: SSLException) { }
            finally { removed.disconnect() }
            // Model a private anchor supplied by the host before Kylok installs its user-store copy.
            val baselineFile=directory.resolve("baseline.p12")
            val baseline=KeyStore.getInstance("PKCS12").apply { load(null,null);setCertificateEntry("host",keys.getCertificate("server")) }
            baselineFile.outputStream().use { baseline.store(it,password) }
            System.setProperty("javax.net.ssl.trustStore",baselineFile.path)
            System.setProperty("javax.net.ssl.trustStorePassword",String(password))
            System.setProperty("javax.net.ssl.trustStoreType","PKCS12")
            assertTrue(NetworkTrust.baselineContains(certificate))
            state.getJSONObject("network_cas").getJSONObject(caDigest(certificate)).put("trust_baseline",true)
            val preserved=URL("https://localhost:${server.address.port}/").openConnection() as HttpsURLConnection
            preserved.sslSocketFactory=NetworkTrust.factory(state,NetworkPolicy.getDefaultInstance())!!;preserved.connectTimeout=2000;preserved.readTimeout=2000
            try { assertEquals(204,preserved.responseCode) } finally { preserved.disconnect() }
            // A retired managed root stays excluded even if the platform's default anchor cache still contains it.
            state.put("network_cas",JSONObject()).put("network_untrusted_cas",JSONObject().put(caDigest(certificate),true))
            val retired=URL("https://localhost:${server.address.port}/").openConnection() as HttpsURLConnection
            retired.sslSocketFactory=NetworkTrust.factory(state,NetworkPolicy.getDefaultInstance())!!;retired.connectTimeout=2000;retired.readTimeout=2000
            try { retired.responseCode;fail("retired root survived default trust cache") } catch (_: SSLException) { }
            finally { retired.disconnect() }
            state.put("network_current",encode(DesiredNetworkPolicy.newBuilder().setRevision(1).setPolicy(proposed).build()))
                .put("network_transaction",JSONObject().put("intent",encode(DesiredNetworkPolicy.newBuilder().setRevision(2).setPolicy(NetworkPolicy.getDefaultInstance()).build())))
            val recovering=URL("https://localhost:${server.address.port}/").openConnection() as HttpsURLConnection
            recovering.sslSocketFactory=NetworkTrust.factory(state)!!;recovering.connectTimeout=2000;recovering.readTimeout=2000
            try { recovering.responseCode;fail("stale rollback policy reauthorized a retired root") } catch (_: SSLException) { }
            finally { recovering.disconnect() }
            assertSame(originalTrust,HttpsURLConnection.getDefaultSSLSocketFactory())
            assertSame(originalVerifier,HttpsURLConnection.getDefaultHostnameVerifier())
        } finally {
            for((name,value)in originalProperties) if(value==null)System.clearProperty(name) else System.setProperty(name,value)
            server.stop(0);directory.deleteRecursively()
        }
    }
}
