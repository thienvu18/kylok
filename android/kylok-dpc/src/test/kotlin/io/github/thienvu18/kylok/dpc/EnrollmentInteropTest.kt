// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import io.github.thienvu18.kylok.proto.v1.*
import io.grpc.Status
import io.grpc.StatusRuntimeException
import io.grpc.okhttp.OkHttpChannelBuilder
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeNotNull
import org.junit.Test
import java.io.File
import java.net.URI
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.cert.CertificateFactory
import java.security.spec.ECGenParameterSpec
import javax.net.ssl.SSLContext
import javax.net.ssl.TrustManagerFactory
import javax.net.ssl.HttpsURLConnection

/** Real HTTP2 gRPC over HTTP or HTTPS, Kotlin/OkHttp → Connect-Go + SQLite. */
class EnrollmentInteropTest {
    @Test fun enrollmentResponseLossInventoryAndErrors() {
        val qrPath = System.getenv("KYLOK_INTEROP_QR")
        val certPath = System.getenv("KYLOK_INTEROP_CERT")
        assumeNotNull(qrPath)
        val qr = JSONObject(File(qrPath!!).readText()).getJSONObject("android.app.extra.PROVISIONING_ADMIN_EXTRAS_BUNDLE")
        val uri = URI(qr.getString("kylok.server"))
        val previousTrust = HttpsURLConnection.getDefaultSSLSocketFactory()
        val client = if (uri.scheme == "http") AgentClient.connect(uri.toString()) else {
            // Test-only CA. Production HTTPS keeps the host's Android trust policy.
            assumeNotNull(certPath)
            val store = KeyStore.getInstance(KeyStore.getDefaultType()).apply { load(null) }
            File(certPath!!).inputStream().use {
                store.setCertificateEntry("fixture", CertificateFactory.getInstance("X.509").generateCertificate(it))
            }
            val tm = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm()).apply { init(store) }
            val ssl = SSLContext.getInstance("TLS").apply { init(null, tm.trustManagers, null) }
            HttpsURLConnection.setDefaultSSLSocketFactory(ssl.socketFactory)
            AgentClient(OkHttpChannelBuilder.forAddress(uri.host, uri.port).sslSocketFactory(ssl.socketFactory).build())
        }
        try { client.use { client ->
            val keys = KeyPairGenerator.getInstance("EC").apply { initialize(ECGenParameterSpec("secp256r1")) }.generateKeyPair()
            val inventory = Inventory.newBuilder().setApiLevel(35).setModel("JVM interoperability fixture")
                .setBuildFingerprint("unit-test/not-device-evidence").setAgentVersion("interop/1").setDeviceOwner(true).addCapabilities("install_apk").addCapabilities("command.reboot").addCapabilities("network.lifecycle").build()
            val attempt = "jvm-attempt-0123456789"
            val token = qr.getString("kylok.enrollment_token")
            val first = client.enroll(token, attempt, keys.public.encoded, inventory)
            // Discard the credential as if the initial response was lost. Retry must return the same identity.
            val recovered = client.enroll(token, attempt, keys.public.encoded, inventory)
            assertEquals(first, recovered)
            val credential = EnrollmentCrypto.open(keys.private, recovered, attempt)
            val syncRequest = SyncRequest.newBuilder().setDeviceId(recovered.device.id).setInventory(inventory).setProtocolVersion(1).setSequence(1).build()
            val sync = client.sync(syncRequest, credential)
            assertEquals(DeviceState.DEVICE_STATE_MANAGED, sync.device.state)
            assertEquals("interop", sync.device.scopeId)
            assertEquals(inventory, sync.device.inventory)
            try { client.enroll(token, "another-attempt-0123456789", keys.public.encoded, inventory); fail("Replay accepted") }
            catch (error: StatusRuntimeException) { assertEquals(Status.Code.ALREADY_EXISTS, error.status.code) }
            try { client.sync(syncRequest, "x".repeat(43)); fail("Bad credential accepted") }
            catch (error: StatusRuntimeException) { assertEquals(Status.Code.UNAUTHENTICATED, error.status.code) }
            val binary = System.getenv("KYLOK_INTEROP_SERVER")
            assumeNotNull(binary)
            fun command(vararg args: String): String {
                val process = ProcessBuilder(listOf(binary!!, *args, "-public-url", uri.toString(), "-apk", System.getenv("KYLOK_INTEROP_APK"),
                    "-data", System.getenv("KYLOK_INTEROP_DATA"), "-scope", "interop", "-allow-debug-apk", "-network-secrets", System.getenv("KYLOK_INTEROP_SECRETS"))).redirectErrorStream(true).start()
                val output = process.inputStream.bufferedReader().readText()
                assertEquals(output, 0, process.waitFor())
                return output
            }
            val artifact = JSONObject(command("register", "-application-apk", System.getenv("KYLOK_INTEROP_APK")))
            command("assign", "-device", recovered.device.id, "-digest", artifact.getString("sha256"), "-request-key", "jvm-install", "-timeout", "1h")
            val directory = java.nio.file.Files.createTempDirectory("kylok-jvm-install").toFile()
            val policyFile = File(directory, "policy.json")
            val packageName = artifact.getString("package_name")
            policyFile.writeText("""{"required_packages":["$packageName"],"camera_disabled":true,"managed_configurations":[{"package_name":"$packageName","values":{"entries":[{"key":"display_name","value":{"string":"LAN configuration"}},{"key":"allow_refresh","value":{"boolean":false}},{"key":"limit","value":{"integer":7}},{"key":"tags","value":{"strings":["a","b"]}},{"key":"nested","value":{"bundle":{"entries":[{"key":"child","value":{"string":"value"}}]}}},{"key":"groups","value":{"bundles":[{"entries":[{"key":"enabled","value":{"boolean":true}}]}]}}]}}],"lock_task":{"packages":["$packageName"],"kiosk_package":"$packageName"}}""")
            command("set-policy", "-device", recovered.device.id, "-request-key", "jvm-policy", "-policy", policyFile.path)
            val storage = MemoryState(JSONObject().put("device_id", recovered.device.id).put("scope_id", "interop").put("sequence", 1))
            val checkpoints = SyncCheckpoint(storage)
            val request = checkpoints.request(inventory)
            val desired = client.sync(request, credential)
            checkpoints.accept(desired)
            assertEquals(1, desired.operationsCount)
            var observed: InstalledApplication? = null
            var commits = 0
            val platform = object : InstallPlatform {
                override fun now() = System.currentTimeMillis()
                override fun isOwner() = true
                override fun installed(packageName: String) = observed
                override fun download(op: Operation, progress: (Long) -> Unit) {
                    ArtifactDownload.fetch(uri.toString(), credential, recovered.device.id, op, File(directory, "artifact.apk"), progress = progress)
                }
                override fun verify(op: Operation) { assertEquals(op.artifact.sizeBytes, File(directory, "artifact.apk").length()) }
                override fun createSession(op: Operation) = 1
                override fun writeSession(id: Int, op: Operation) { }
                override fun sessionExists(id: Int) = true
                override fun sessionSealed(id: Int) = commits > 0
                override fun commit(id: Int, operationID: String) { commits++ }
                override fun abandon(id: Int) { }
                override fun cleanup(retained: Set<String>, sessions: Set<Int>) { }
            }
            val policyValues = mutableMapOf<String, String>()
            val policy = PolicyEngine(storage, object : PolicyPlatform {
                override fun now() = System.currentTimeMillis()
                override fun isOwner() = true
                override fun installed(packageName: String) = observed
                override fun check(setting: PolicySetting) { }
                override fun capture(setting: PolicySetting) = observe(setting)
                override fun observe(setting: PolicySetting) = policyValues[setting.id] ?: when (setting.kind) {
                    PolicyKind.LOCK_TASK -> lockTaskValue(emptyList(), "")
                    PolicyKind.MANAGED_CONFIGURATION -> ConfigurationValues.journal(ConfigurationBundle.getDefaultInstance())
                    else -> "false"
                }
                override fun apply(setting: PolicySetting) { policyValues[setting.id] = setting.value }
            })
            try {
                assertTrue(policy.run()) // Required app is absent; kiosk must wait.
                assertFalse(policyValues.containsKey("lock_task"))
                assertTrue(InstallEngine(storage, platform).run())
                checkpoints.accept(client.sync(checkpoints.request(inventory), credential))
                val target = desired.operationsList.single().artifact
                observed = InstalledApplication.newBuilder().setPackageName(target.packageName).setVersionCode(target.versionCode)
                    .addAllSigningCertificates(target.signingCertificatesList).build()
                assertFalse(InstallEngine(storage, platform).run())
                assertFalse(policy.run())
                val completed = client.sync(checkpoints.request(inventory.toBuilder().addApplications(observed).build()), credential)
                checkpoints.accept(completed)
                assertEquals(ComplianceState.COMPLIANCE_STATE_APPLIED, completed.complianceList.single().state)
                assertEquals(0, completed.operationsCount)
                assertEquals(1, commits)
                assertEquals(5, completed.policyComplianceCount)
                assertTrue(completed.policyComplianceList.single { it.ruleId.startsWith("configuration:") }.observedValue.startsWith("sha256:"))
                assertTrue(completed.policyComplianceList.all { it.state == ComplianceState.COMPLIANCE_STATE_APPLIED })
                val operations = org.json.JSONArray(command("operations", "-device", recovered.device.id))
                assertEquals("SUCCEEDED", operations.getJSONObject(0).getString("state"))
                val policyStatus = JSONObject(command("policy", "-device", recovered.device.id))
                assertEquals(2, policyStatus.getJSONObject("desired").getLong("revision"))
                policyFile.writeText("{}")
                command("set-policy", "-device", recovered.device.id, "-request-key", "jvm-policy-release", "-policy", policyFile.path)
                val installedInventory = inventory.toBuilder().addApplications(observed).build()
                checkpoints.accept(client.sync(checkpoints.request(installedInventory), credential))
                assertFalse(policy.run())
                val released = client.sync(checkpoints.request(installedInventory), credential)
                checkpoints.accept(released)
                assertEquals("cleanup", released.policyComplianceList.single().ruleId)
                assertEquals(ComplianceState.COMPLIANCE_STATE_APPLIED, released.policyComplianceList.single().state)
                assertEquals("false", policyValues["camera"])
                assertEquals(ConfigurationValues.journal(ConfigurationBundle.getDefaultInstance()), policyValues["configuration:$packageName"])
                assertEquals(lockTaskValue(emptyList(), ""), policyValues["lock_task"])
                // Use the example's public SDK boundary for frozen fleet submission and scheduled cancellation.
                val members = File(directory,"members.json").apply { writeText(org.json.JSONArray(listOf(recovered.device.id)).toString()) }
                command("set-group","-group","interop-group","-document",members.path)
                val batchFile = File(directory,"batch.json").apply { writeText(JSONObject().put("id","interop-batch").put("group_id","interop-group")
                    .put("artifact_sha256",artifact.getString("sha256")).put("not_before",java.time.Instant.now().minusSeconds(1).toString())
                    .put("deadline",java.time.Instant.now().plusSeconds(3600).toString()).toString()) }
                command("fleet-create","-document",batchFile.path)
                val batch = JSONObject(command("fleet-advance","-batch","interop-batch","-limit","1"))
                assertTrue(batch.getBoolean("complete"))
                checkpoints.accept(client.sync(checkpoints.request(installedInventory),credential))
                assertFalse(InstallEngine(storage,platform).run())
                checkpoints.accept(client.sync(checkpoints.request(installedInventory),credential))
                assertEquals(1,commits) // Already observed installed bytes need no second installer effect.
                val scheduled = JSONObject(command("assign-window","-device",recovered.device.id,"-digest",artifact.getString("sha256"),"-request-key","interop-window",
                    "-not-before",java.time.Instant.now().plusSeconds(3600).toString(),"-deadline",java.time.Instant.now().plusSeconds(7200).toString()))
                val early = client.sync(checkpoints.request(installedInventory),credential);checkpoints.accept(early)
                assertEquals(0,early.operationsCount)
                assertEquals("CANCELLED",JSONObject(command("cancel-operation","-device",recovered.device.id,"-operation",scheduled.getString("id"))).getString("state"))

                // Android effects stay modeled; transport, dispatch/report tokens and server state are real.
                val reboot = JSONObject(command("command-submit","-device",recovered.device.id,"-kind","REBOOT","-request-key","interop-reboot","-timeout","1h"))
                checkpoints.accept(client.sync(checkpoints.request(installedInventory),credential))
                var boots = 7L; var reboots = 0
                val commands = CommandEngine(storage,object : CommandPlatform {
                    override fun now() = System.currentTimeMillis()
                    override fun processID() = "interop-process"
                    override fun isOwner() = true
                    override fun bootCount() = boots
                    override fun isSecure() = true
                    override fun isLocked() = false
                    override fun reboot() { reboots++;boots++ }
                    override fun lock() { fail("lock not assigned") }
                })
                assertFalse(commands.run())
                checkpoints.accept(client.sync(checkpoints.request(installedInventory),credential))
                assertEquals(1,reboots)
                assertEquals("SUCCEEDED",JSONObject(command("command","-device",recovered.device.id,"-command-id",reboot.getString("id"))).getString("state"))
                val networkFile = File(directory,"network.json").apply { writeText("""{"primary_id":"office","profiles":[{"id":"office","ssid":"Office","security":"WPA2_PSK","secret_ref":"wifi-v1"}]}""") }
                val fixtureCA=File(certPath!!).inputStream().use { CertificateFactory.getInstance("X.509").generateCertificate(it).encoded }
                networkFile.writeText(JSONObject(networkFile.readText()).put("certificates",org.json.JSONArray().put(JSONObject().put("id","fixture-root")
                    .put("der",java.util.Base64.getEncoder().encodeToString(fixtureCA)))).toString())
                command("set-network","-device",recovered.device.id,"-request-key","interop-network","-document",networkFile.path)
                val networking = client.sync(checkpoints.request(installedInventory),credential);checkpoints.accept(networking)
                assertEquals("jvm-passphrase",networking.desiredNetworkPolicy.policy.profilesList.single().passphrase.toStringUtf8())
                val savedNetworks = mutableMapOf(1 to SavedNetwork(1,"Recovery","WPA2_PSK"));val installedCAs=mutableSetOf<String>();var activeNetwork = 1;var probeCount = 0
                val networks = NetworkEngine(storage,object : NetworkPlatform {
                    override fun check() { }
                    override fun now() = System.currentTimeMillis()
                    override fun configured() = savedNetworks.values.toList()
                    override fun callerConfigured() = savedNetworks.values.filter { it.id != 1 }
                    override fun currentNetworkID() = activeNetwork
                    override fun upsert(profile: WifiProfile,id: Int?): Int {
                        assertEquals("jvm-passphrase",profile.passphrase.toStringUtf8())
                        savedNetworks[2] = SavedNetwork(2,profile.ssid,profile.security);return 2
                    }
                    override fun select(id: Int) { activeNetwork=id }
                    override fun remove(network: SavedNetwork) { assertNotEquals(activeNetwork,network.id);assertNotEquals(1,network.id);savedNetworks.remove(network.id) }
                    override fun hasCA(der: ByteArray) = caDigest(der) in installedCAs
                    override fun trustsCAByDefault(der: ByteArray) = uri.scheme=="https" && der.contentEquals(fixtureCA)
                    override fun installCA(der: ByteArray) { installedCAs += caDigest(der) }
                    override fun removeCA(der: ByteArray) { installedCAs -= caDigest(der) }
                    override fun probe(id: Int,policy: NetworkPolicy): Boolean {
                        if(id!=activeNetwork)return false
                        val connection = uri.resolve("/network-probe").toURL().openConnection() as java.net.HttpURLConnection
                        connection.connectTimeout=2000;connection.readTimeout=2000;connection.instanceFollowRedirects=false
                        connection.setRequestProperty("Authorization","Bearer $credential")
                        connection.setRequestProperty("X-Kylok-Device-ID",recovered.device.id)
                        connection.setRequestProperty("X-Kylok-Probe","n".repeat(43))
                        return try { probeCount++;connection.responseCode==204 && connection.getHeaderField("X-Kylok-Probe")=="n".repeat(43) } finally { connection.disconnect() }
                    }
                })
                assertFalse(networks.run())
                checkpoints.accept(client.sync(checkpoints.request(installedInventory),credential))
                assertEquals("APPLIED",JSONObject(command("network","-device",recovered.device.id)).getString("state"))
                assertTrue(probeCount>=2)
                networkFile.writeText("{}")
                command("set-network","-device",recovered.device.id,"-request-key","interop-network-release","-document",networkFile.path)
                checkpoints.accept(client.sync(checkpoints.request(installedInventory),credential))
                assertFalse(networks.run())
                checkpoints.accept(client.sync(checkpoints.request(installedInventory),credential))
                assertTrue(installedCAs.isEmpty())
                assertEquals(setOf(1),savedNetworks.keys)
                assertEquals("APPLIED",JSONObject(command("network","-device",recovered.device.id)).getString("state"))
            } finally { directory.deleteRecursively() }
        }
        } finally { HttpsURLConnection.setDefaultSSLSocketFactory(previousTrust) }
    }
}
