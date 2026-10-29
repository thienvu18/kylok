// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import com.google.protobuf.ByteString
import io.github.thienvu18.kylok.proto.v1.*
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.security.cert.CertificateFactory
import java.security.cert.X509Certificate

class NetworkEngineTest {
    private val caA = javaClass.getResourceAsStream("/network-ca-a.der")!!.use { it.readBytes() }
    private val caB = javaClass.getResourceAsStream("/network-ca-b.der")!!.use { it.readBytes() }
    private val date = (CertificateFactory.getInstance("X.509").generateCertificate(caA.inputStream()) as X509Certificate).notBefore.time + 1000
    private fun profile(pass: String = "good-pass", ssid: String = "Office") = WifiProfile.newBuilder().setId("office").setSsid(ssid)
        .setSecurity("WPA2_PSK").setPassphrase(ByteString.copyFromUtf8(pass)).build()
    private fun policy(profile: WifiProfile = profile(), cas: List<ByteArray> = emptyList()): NetworkPolicy = NetworkPolicy.newBuilder().setPrimaryId(profile.id).addProfiles(profile)
        .addAllCertificates(cas.mapIndexed { index, bytes -> CACertificate.newBuilder().setId("root-$index").setDer(ByteString.copyFrom(bytes)).build() }).build()
    private fun state(policy: NetworkPolicy = policy(), revision: Long = 1) = MemoryState(JSONObject().put("snapshot", encode(SyncResponse.newBuilder()
        .setDesiredNetworkPolicy(DesiredNetworkPolicy.newBuilder().setRevision(revision).setPolicy(policy)).build())))
    private fun desired(storage: StateAccess, policy: NetworkPolicy, revision: Long) = storage.update { it.put("snapshot", encode(SyncResponse.newBuilder()
        .setDesiredNetworkPolicy(DesiredNetworkPolicy.newBuilder().setRevision(revision).setPolicy(policy)).build())) }
    private fun report(storage: StateAccess) = NetworkReport.parseFrom(decode(storage.read().getString("network_report")))
    private class Death : Error()
    private inner class Platform : NetworkPlatform {
        val configs = mutableMapOf(1 to SavedNetwork(1, "Recovery", "WPA2_PSK"))
        val profiles = mutableMapOf<Int, WifiProfile>()
        val caller = mutableSetOf<Int>(); val certificates = mutableSetOf<String>(); val baselineTrust = mutableSetOf<String>(); val effects = mutableListOf<String>()
        var active = 1; var next = 2; var permitted = true; var reachable = true; var time = date; var neededCA: String? = null
        var replaceOnSecurityChange = false
        var expectedPass = "good-pass"; var fail: ((String) -> Unit)? = null
        override fun check() { if (!permitted) throw NetworkRejected("NETWORK_PERMISSION_REQUIRED") }
        override fun now() = time
        override fun configured() = configs.values.toList()
        override fun callerConfigured() = configs.values.filter { it.id in caller }
        override fun currentNetworkID() = active
        override fun upsert(profile: WifiProfile, id: Int?): Int {
            val replaced = replaceOnSecurityChange && id != null && configs[id]?.security != profile.security
            val key = if (replaced) next++ else id ?: next++
            fail?.invoke("before-write")
            configs[key] = SavedNetwork(key, profile.ssid, profile.security); profiles[key] = profile; caller += key
            effects += "write:$key"; fail?.invoke("after-write"); if (replaced) throw NetworkRejected("NETWORK_WRITE_REJECTED"); return key
        }
        override fun select(id: Int) { check(configs.containsKey(id)); active = id; effects += "select:$id"; fail?.invoke("after-select") }
        override fun remove(network: SavedNetwork) {
            assertNotEquals("last active route removed", active, network.id); assertTrue(network.id in caller)
            configs.remove(network.id); profiles.remove(network.id); caller.remove(network.id); effects += "remove:${network.id}"; fail?.invoke("after-remove")
        }
        override fun hasCA(der: ByteArray) = caDigest(der) in certificates
        override fun trustsCAByDefault(der: ByteArray) = caDigest(der) in baselineTrust || hasCA(der)
        override fun installCA(der: ByteArray) { certificates += caDigest(der); effects += "install-ca"; fail?.invoke("after-install-ca") }
        override fun removeCA(der: ByteArray) { certificates -= caDigest(der); effects += "remove-ca"; fail?.invoke("after-remove-ca") }
        override fun probe(id: Int, policy: NetworkPolicy): Boolean {
            val password = profiles[id]?.passphrase?.toStringUtf8()
            val trusted = neededCA == null || neededCA in baselineTrust || policy.certificatesList.any { caDigest(it.der.toByteArray()) == neededCA }
            val success = reachable && active == id && configs.containsKey(id) && (password == null || password == expectedPass) && trusted
            effects += "probe:$id:$success"; fail?.invoke("after-probe"); return success
        }
    }
    @Test fun applyRotateCredentialsAndReleasePreserveUnmanagedNetworks() {
        val storage = state(); val platform = Platform()
        platform.configs[77] = SavedNetwork(77,"Personal","OPEN")
        assertFalse(NetworkEngine(storage,platform).run())
        assertEquals("APPLIED",report(storage).state); assertTrue(report(storage).connectivityVerified)
        val firstID = platform.active
        assertEquals("Recovery",platform.configs[1]!!.ssid); assertEquals("Personal",platform.configs[77]!!.ssid)
        platform.expectedPass = "rotated-pass"
        desired(storage,policy(profile("rotated-pass")),2)
        assertFalse(NetworkEngine(storage,platform).run())
        assertEquals(firstID,platform.active); assertEquals("rotated-pass",platform.profiles[firstID]!!.passphrase.toStringUtf8())
        assertEquals("APPLIED",report(storage).state)
        desired(storage,NetworkPolicy.getDefaultInstance(),3)
        assertFalse(NetworkEngine(storage,platform).run())
        assertEquals(1,platform.active); assertEquals(setOf(1,77),platform.configs.keys)
        assertEquals("APPLIED",report(storage).state); assertTrue(report(storage).cleanupComplete)
    }
    @Test fun badCredentialsRollbackAndDoNotRepeatedlySwitchForSameRevision() {
        val storage = state(); val platform = Platform(); NetworkEngine(storage,platform).run()
        val id = platform.active
        desired(storage,policy(profile("wrong-pass")),2)
        assertFalse(NetworkEngine(storage,platform).run())
        assertEquals("FAILED",report(storage).state); assertEquals(1,platform.active)
        assertEquals("good-pass",platform.profiles[id]!!.passphrase.toStringUtf8())
        val count = platform.effects.size
        assertFalse(NetworkEngine(MemoryState(storage.read()),platform).run()); assertEquals(count,platform.effects.size)
        assertEquals("Recovery",platform.configs[1]!!.ssid)
    }
    @Test fun missingPermissionsRecoveryAndUnmanagedCollisionsHaveNoDestructiveEffects() {
        for (problem in listOf("permission","recovery","collision")) {
            val storage = state(); val platform = Platform()
            when(problem) {
                "permission" -> platform.permitted = false
                "recovery" -> platform.active = -1
                "collision" -> platform.configs[9] = SavedNetwork(9,"Office","WPA2_PSK")
            }
            NetworkEngine(storage,platform).run()
            assertEquals("FAILED",report(storage).state)
            assertFalse(platform.effects.any { it.startsWith("write:") || it.startsWith("remove:") })
            assertTrue(platform.configs.containsKey(1))
        }
    }
    @Test fun privateCARotationPreservesPreexistingTrustAndBlocksUnsafeRelease() {
        val storage = state(policy(cas=listOf(caA))); val platform = Platform()
        platform.certificates += caDigest(caA) // This CA existed before Kylok claimed policy ownership.
        NetworkEngine(storage,platform).run()
        desired(storage,policy(cas=listOf(caA,caB)),2); NetworkEngine(storage,platform).run()
        platform.neededCA = caDigest(caB)
        desired(storage,policy(cas=listOf(caB)),3); NetworkEngine(storage,platform).run()
        assertEquals("APPLIED",report(storage).state)
        assertTrue(platform.hasCA(caA)); assertTrue(platform.hasCA(caB))
        desired(storage,NetworkPolicy.getDefaultInstance(),4)
        assertFalse(NetworkEngine(storage,platform).run()) // Release is rejected and the prior policy is recovered.
        assertEquals("FAILED",report(storage).state); assertFalse(storage.read().has("network_transaction"))
        assertTrue(platform.hasCA(caA)); assertTrue(platform.hasCA(caB)); assertTrue(platform.configs.containsKey(1))
        platform.neededCA = null
        assertFalse(NetworkEngine(storage,platform).recover())
        assertTrue(platform.hasCA(caA)); assertTrue(platform.hasCA(caB)) // Rolled back the release to its last known good policy.
        desired(storage,NetworkPolicy.getDefaultInstance(),5); NetworkEngine(storage,platform).run()
        assertTrue(platform.hasCA(caA)); assertFalse(platform.hasCA(caB))
        assertEquals("APPLIED",report(storage).state)
    }
    @Test fun releasePreservesPrivateTrustAlreadySuppliedByTheHostAPK() {
        val storage = state(policy(cas=listOf(caA))); val platform = Platform()
        platform.baselineTrust += caDigest(caA); platform.neededCA = caDigest(caA)
        assertFalse(NetworkEngine(storage,platform).run());assertTrue(platform.hasCA(caA))
        desired(storage,NetworkPolicy.getDefaultInstance(),2)
        assertFalse(NetworkEngine(storage,platform).run())
        assertEquals("APPLIED",report(storage).state);assertFalse(platform.hasCA(caA))
        assertFalse(storage.read().objectValue("network_untrusted_cas").has(caDigest(caA)))
        assertEquals(1,platform.active)
    }
    @Test fun expiredCertificatesNeverChangeTrustOrRemoveWorkingProfiles() {
        val storage = state(policy(cas=listOf(caA))); val platform = Platform()
        platform.time = (CertificateFactory.getInstance("X.509").generateCertificate(caA.inputStream()) as X509Certificate).notAfter.time + 1
        NetworkEngine(storage,platform).run()
        assertEquals("CA_NOT_VALID",report(storage).reason); assertTrue(platform.effects.isEmpty()); assertEquals(setOf(1),platform.configs.keys)
    }
    @Test fun processDeathAfterNetworkCreationRecoversTheEffectCheckpointGap() {
        val storage = state(); val platform = Platform()
        platform.fail = { if(it=="after-write") throw Death() }
        try { NetworkEngine(storage,platform).run(); fail("fault not injected") } catch (_: Death) { }
        assertEquals(2,platform.configs.size)
        platform.fail = null
        val restarted = MemoryState(storage.read())
        assertFalse(NetworkEngine(restarted,platform).recover())
        assertEquals(setOf(1),platform.configs.keys); assertEquals(1,platform.active)
        assertEquals("FAILED",report(restarted).state)
    }
    @Test fun processDeathAfterCredentialUpdateRestoresPreviousCredentials() {
        val storage = state(); val platform = Platform(); NetworkEngine(storage,platform).run()
        val id = platform.active
        platform.expectedPass = "next-pass"; desired(storage,policy(profile("next-pass")),2)
        platform.fail = { if(it=="after-write") throw Death() }
        try { NetworkEngine(storage,platform).run(); fail("fault not injected") } catch (_: Death) { }
        platform.fail = null
        val restarted = MemoryState(storage.read())
        assertFalse(NetworkEngine(restarted,platform).recover())
        assertEquals("good-pass",platform.profiles[id]!!.passphrase.toStringUtf8()); assertEquals(1,platform.active)
    }
    @Test fun rejectedSecurityUpdateWithANewNetworkIDRecoversOwnedCollision() {
        val storage = state(); val platform = Platform(); NetworkEngine(storage,platform).run()
        val original = platform.active
        platform.replaceOnSecurityChange = true
        desired(storage,policy(profile().toBuilder().setSecurity("WPA3_SAE").build()),2)
        assertFalse(NetworkEngine(storage,platform).run())
        assertEquals("FAILED",report(storage).state);assertEquals(1,platform.active)
        assertEquals(setOf(1,original),platform.configs.keys)
        assertEquals("WPA2_PSK",platform.configs[original]!!.security)
    }
    @Test fun processDeathAfterVerifiedCleanupResumesVerifiedIntentWithoutRemovingRecovery() {
        val storage = state(); val platform = Platform(); NetworkEngine(storage,platform).run()
        desired(storage,policy(profile(ssid="OtherOffice")),2)
        platform.fail = { if(it=="after-remove") throw Death() }
        try { NetworkEngine(storage,platform).run(); fail("fault not injected") } catch (_: Death) { }
        assertTrue(storage.read().getJSONObject("network_transaction").getBoolean("verified"))
        platform.fail = null
        val restarted = MemoryState(storage.read())
        assertFalse(NetworkEngine(restarted,platform).recover())
        assertEquals("APPLIED",report(restarted).state); assertEquals("OtherOffice",platform.configs[platform.active]!!.ssid)
        assertEquals("Recovery",platform.configs[1]!!.ssid)
    }
    @Test fun lostPrimaryRecoversManagementWithoutRemovingItsConfiguration() {
        val storage = state(); val platform = Platform(); NetworkEngine(storage,platform).run()
        val primary = platform.active
        platform.expectedPass = "router-changed-password"; platform.active = -1
        assertFalse(NetworkEngine(storage,platform).recoverConnectivity())
        assertEquals(1,platform.active); assertTrue(platform.configs.containsKey(primary))
        assertEquals("RECOVERY_ROUTE_ACTIVE",report(storage).reason)
        val count = platform.effects.size
        assertFalse(NetworkEngine(storage,platform).run()); assertEquals(count,platform.effects.size)
        desired(storage,policy(profile("router-changed-password")),2)
        assertFalse(NetworkEngine(storage,platform).run()); assertEquals("APPLIED",report(storage).state)
    }
    @Test fun certificateInstallAndRetirementEffectGapsRecoverFromTheirOwnershipJournal() {
        for(point in listOf("after-install-ca","after-remove-ca")) {
            val storage=state(policy(cas=listOf(caA)));val platform=Platform()
            if(point=="after-remove-ca") {
                NetworkEngine(storage,platform).run()
                desired(storage,policy(cas=listOf(caB)),2)
            }
            platform.fail={ if(it==point)throw Death() }
            try { NetworkEngine(storage,platform).run();fail("CA effect fault not injected") } catch (_: Death) { }
            platform.fail=null
            val restarted=MemoryState(storage.read())
            assertFalse(NetworkEngine(restarted,platform).recover())
            assertFalse(platform.hasCA(caA));assertTrue(platform.configs.containsKey(1))
            if(point=="after-install-ca") {
                assertEquals("FAILED",report(restarted).state);assertEquals(1,platform.active)
            } else {
                assertEquals("APPLIED",report(restarted).state);assertTrue(platform.hasCA(caB))
            }
        }
    }
    @Test fun trustFingerprintLimitRejectsNewRootsBeforeCertificateEffects() {
        val storage=state(policy(cas=listOf(caA)));val platform=Platform()
        storage.update { state ->
            val retired=state.objectValue("network_untrusted_cas")
            for(index in 0 until 256)retired.put("%064x".format(index),true)
        }
        assertFalse(NetworkEngine(storage,platform).run())
        assertEquals("TRUST_HISTORY_LIMIT",report(storage).reason)
        assertFalse(platform.hasCA(caA));assertTrue(platform.effects.none { it=="install-ca" || it.startsWith("write:") })
        assertEquals(1,platform.active)
    }
    @Test fun everyCheckpointFaultPreservesRecoveryAndNeverDeletesUnmanagedSettings() {
        for (after in listOf(false,true)) for (point in 1..16) {
            val base = state(policy(cas=listOf(caA))); val platform = Platform(); var calls = 0
            val crashing = object : StateAccess {
                override fun read() = base.read()
                override fun update(change:(JSONObject)->Unit) {
                    calls++
                    if(calls==point&&!after) throw Death()
                    base.update(change)
                    if(calls==point&&after) throw Death()
                }
            }
            try { NetworkEngine(crashing,platform).run() } catch (_: Death) { }
            val restarted = MemoryState(base.read())
            NetworkEngine(restarted,platform).recover(); NetworkEngine(restarted,platform).run()
            assertEquals("Recovery",platform.configs[1]!!.ssid)
            assertFalse(restarted.read().has("network_transaction"))
            assertTrue(platform.configs.containsKey(platform.active))
            assertTrue(report(restarted).state in setOf("APPLIED","FAILED"))
        }
    }

    @Test
    fun testNetworkValidationEdgeCases() {
        val platform = Platform()

        // Profiles count limit > 8
        val b = NetworkPolicy.newBuilder()
        for (i in 0..8) {
            b.addProfiles(profile(ssid = "S$i").toBuilder().setId("p$i"))
        }
        b.setPrimaryId("p0")
        var storage = state(b.build())
        assertFalse(NetworkEngine(storage, platform).run())
        assertEquals("NETWORK_LIMIT", report(storage).reason)

        // Invalid profile ID
        val badIdPolicy = NetworkPolicy.newBuilder()
            .addProfiles(profile().toBuilder().setId("123bad"))
            .setPrimaryId("123bad").build()
        storage = state(badIdPolicy)
        assertFalse(NetworkEngine(storage, platform).run())
        assertEquals("INVALID_PROFILE", report(storage).reason)

        // Invalid SSID
        val badSsidPolicy = NetworkPolicy.newBuilder()
            .addProfiles(profile(ssid = ""))
            .setPrimaryId("office").build()
        storage = state(badSsidPolicy)
        assertFalse(NetworkEngine(storage, platform).run())
        assertEquals("INVALID_PROFILE", report(storage).reason)

        // Invalid secrets
        val badOpen = NetworkPolicy.newBuilder()
            .addProfiles(profile(pass = "notempty").toBuilder().setSecurity("OPEN"))
            .setPrimaryId("office").build()
        storage = state(badOpen)
        assertFalse(NetworkEngine(storage, platform).run())
        assertEquals("INVALID_PROFILE", report(storage).reason)

        val badWpa2 = NetworkPolicy.newBuilder()
            .addProfiles(profile(pass = "short"))
            .setPrimaryId("office").build()
        storage = state(badWpa2)
        assertFalse(NetworkEngine(storage, platform).run())
        assertEquals("INVALID_PROFILE", report(storage).reason)

        val badWpa3 = NetworkPolicy.newBuilder()
            .addProfiles(profile(pass = "\u0000").toBuilder().setSecurity("WPA3_SAE"))
            .setPrimaryId("office").build()
        storage = state(badWpa3)
        assertFalse(NetworkEngine(storage, platform).run())
        assertEquals("INVALID_PROFILE", report(storage).reason)

        // Primary ID not in profiles
        val missingPrimary = NetworkPolicy.newBuilder()
            .addProfiles(profile())
            .setPrimaryId("unknown").build()
        storage = state(missingPrimary)
        assertFalse(NetworkEngine(storage, platform).run())
        assertEquals("PRIMARY_REQUIRED", report(storage).reason)

        // Duplicate CA ID
        val dupCa = NetworkPolicy.newBuilder()
            .addProfiles(profile())
            .setPrimaryId("office")
            .addCertificates(CACertificate.newBuilder().setId("ca1").setDer(com.google.protobuf.ByteString.copyFrom(caA)))
            .addCertificates(CACertificate.newBuilder().setId("ca1").setDer(com.google.protobuf.ByteString.copyFrom(caB)))
            .build()
        storage = state(dupCa)
        assertFalse(NetworkEngine(storage, platform).run())
        assertEquals("DUPLICATE_CA", report(storage).reason)

        // Invalid CA bytes
        assertThrows(NetworkRejected::class.java) {
            networkCertificate(byteArrayOf(1, 2, 3), date)
        }
        assertThrows(NetworkRejected::class.java) {
            networkCertificate(ByteArray(0), date)
        }
        assertThrows(NetworkRejected::class.java) {
            networkCertificate(ByteArray(17 * 1024), date)
        }
        assertThrows(NetworkRejected::class.java) {
            networkCertificate(caA, 0L) // expired / future date
        }
    }

    @Test
    fun testRecoverConnectivityBranches() {
        val platform = Platform()
        val storage = state(policy())
        val engine = NetworkEngine(storage, platform)

        // No transaction, no current policy -> returns false
        assertFalse(engine.recoverConnectivity())

        // Successfully applied policy
        assertTrue(!engine.run() && report(storage).state == "APPLIED")
        assertEquals("APPLIED", report(storage).state)

        // Primary route healthy -> returns false
        assertFalse(engine.recoverConnectivity())

        // Primary route probe fails, recovery route succeeds
        val platformWithRecovery = object : NetworkPlatform by platform {
            override fun probe(id: Int, policy: NetworkPolicy): Boolean {
                return id == 1 // recovery route succeeds, primary fails
            }
        }
        storage.update { it.remove("network_failed_revision") }
        val eng2 = NetworkEngine(storage, platformWithRecovery)
        assertFalse(eng2.recoverConnectivity())
        assertEquals("FAILED", report(storage).state)
        assertEquals("RECOVERY_ROUTE_ACTIVE", report(storage).reason)
    }

    @Test
    fun testRunAndRecoverAdditionalBranches() {
        val platform = Platform()
        val storage = MemoryState(JSONObject())
        val engine = NetworkEngine(storage, platform)

        // desired is null -> run() returns false
        assertFalse(engine.run())

        // network_failed_revision == want.revision -> returns false
        val p = policy()
        desired(storage, p, 10L)
        storage.update { it.put("network_failed_revision", 10L) }
        assertFalse(engine.run())
        storage.update { it.remove("network_failed_revision") }

        // Recovery route SSID collision
        val collide = policy(profile(ssid = "Recovery"))
        desired(storage, collide, 11L)
        assertFalse(engine.run())
        assertEquals("INDEPENDENT_RECOVERY_REQUIRED", report(storage).reason)

        // Permission required (SecurityException)
        val denyPlatform = object : NetworkPlatform by platform {
            override fun check() { throw SecurityException("Denied") }
        }
        desired(storage, p, 12L)
        assertFalse(NetworkEngine(storage, denyPlatform).run())
        assertEquals("UNSUPPORTED", report(storage).state)
        assertEquals("NETWORK_PERMISSION_REQUIRED", report(storage).reason)
    }

    @Test
    fun testRecoverVerifiedTransactionFlows() {
        val platform = Platform()
        val storage = MemoryState(JSONObject())
        val engine = NetworkEngine(storage, platform)

        val prof = profile("good-pass", "ManagedSSID")
        val p = policy(prof)
        val want = DesiredNetworkPolicy.newBuilder().setRevision(15L).setPolicy(p).build()
        val profRecord = JSONObject().put("id", 2).put("profile", encode(prof))
        val recRecord = JSONObject().put("id", 1).put("ssid", "Recovery").put("security", "WPA2_PSK")
        val stagedProfiles = JSONObject().put(prof.id, profRecord)
        val tx = JSONObject().put("intent", encode(want)).put("verified", true)
            .put("profiles", stagedProfiles).put("before_profiles", JSONObject())
        storage.update { it.put("network_transaction", tx).put("network_recovery", recRecord) }

        platform.configs[2] = SavedNetwork(2, "ManagedSSID", "WPA2_PSK")
        platform.caller.add(2)
        platform.active = 2

        // Success path
        assertFalse(engine.recover())
        assertEquals("APPLIED", report(storage).state)
        assertTrue(report(storage).connectivityVerified)

        // Readback mismatch path -> throws NetworkRejected -> reports PENDING, RECOVERY_PENDING, returns true
        val txMismatch = JSONObject().put("intent", encode(want)).put("verified", true)
            .put("profiles", JSONObject()) // empty profiles
            .put("before_profiles", JSONObject())
        storage.update { it.put("network_transaction", txMismatch).put("network_recovery", recRecord) }
        assertTrue(engine.recover())
        assertEquals("PENDING", report(storage).state)
        assertEquals("RECOVERY_PENDING", report(storage).reason)

        // SecurityException path -> returns true, reports RECOVERY_PERMISSION_REQUIRED
        val secPlatform = object : NetworkPlatform by platform {
            override fun check() { throw SecurityException("Denied") }
        }
        val secEngine = NetworkEngine(storage, secPlatform)
        assertTrue(secEngine.recover())
        assertEquals("PENDING", report(storage).state)
        assertEquals("RECOVERY_PERMISSION_REQUIRED", report(storage).reason)

        // Interrupted unverified migration path -> rollback and report FAILED
        val txUnverified = JSONObject().put("intent", encode(want)).put("verified", false)
            .put("profiles", stagedProfiles).put("before_profiles", JSONObject())
        storage.update { it.put("network_transaction", txUnverified).put("network_recovery", recRecord) }
        assertFalse(engine.recover())
        assertEquals("FAILED", report(storage).state)
        assertEquals("INTERRUPTED_MIGRATION_RECOVERED", report(storage).reason)
        assertEquals(15L, storage.read().getLong("network_failed_revision"))
    }

    @Test
    fun testRecoverConnectivityEdgeCases() {
        val platform = Platform()
        val storage = MemoryState(JSONObject())
        val engine = NetworkEngine(storage, platform)

        // Empty current policy -> returns false
        val emptyP = DesiredNetworkPolicy.newBuilder().setRevision(1L).setPolicy(NetworkPolicy.getDefaultInstance()).build()
        storage.update {
            it.put("network_current", encode(emptyP))
                .put("network_recovery", JSONObject().put("id", 1).put("ssid", "Recovery").put("security", "WPA2_PSK"))
        }
        assertFalse(engine.recoverConnectivity())

        // Connect recovery fails with NetworkRejected -> reports PENDING, returns true
        val prof = profile("good-pass", "ManagedSSID")
        val p = policy(prof)
        val want = DesiredNetworkPolicy.newBuilder().setRevision(2L).setPolicy(p).build()
        val profRecord = JSONObject().put("id", 2).put("profile", encode(prof))
        storage.update {
            it.put("network_current", encode(want))
                .put("network_profiles", JSONObject().put(prof.id, profRecord))
        }
        val failProbePlatform = object : NetworkPlatform by platform {
            override fun probe(id: Int, policy: NetworkPolicy): Boolean = false
        }
        assertTrue(NetworkEngine(storage, failProbePlatform).recoverConnectivity())
        assertEquals("PENDING", report(storage).state)
        assertEquals("RECOVERY_PENDING", report(storage).reason)

        // Connect recovery fails with SecurityException -> reports UNSUPPORTED, returns false
        val secPlatform = object : NetworkPlatform by platform {
            override fun probe(id: Int, policy: NetworkPolicy): Boolean { throw SecurityException("Not allowed") }
        }
        assertFalse(NetworkEngine(storage, secPlatform).recoverConnectivity())
        assertEquals("UNSUPPORTED", report(storage).state)
        assertEquals("NETWORK_PERMISSION_REQUIRED", report(storage).reason)
    }

    @Test
    fun testRunCurrentPolicyReapplicationBranches() {
        val platform = Platform()
        val storage = MemoryState(JSONObject())
        val engine = NetworkEngine(storage, platform)

        val prof = profile("good-pass", "ManagedSSID")
        val p = policy(prof, cas = listOf(caA))
        val want = DesiredNetworkPolicy.newBuilder().setRevision(5L).setPolicy(p).build()
        desired(storage, p, 5L)

        val profRecord = JSONObject().put("id", 2).put("profile", encode(prof))
        val recRecord = JSONObject().put("id", 1).put("ssid", "Recovery").put("security", "WPA2_PSK")

        platform.configs[2] = SavedNetwork(2, "ManagedSSID", "WPA2_PSK")
        platform.caller.add(2)
        platform.active = 2
        platform.certificates.add(caDigest(caA))

        storage.update {
            it.put("network_current", encode(want))
                .put("network_profiles", JSONObject().put(prof.id, profRecord))
                .put("network_recovery", recRecord)
        }

        // Current policy matches desired, readback matches, CA exists, probe succeeds -> APPLIED
        assertFalse(engine.run())
        assertEquals("APPLIED", report(storage).state)

        // Readback profile missing from network_profiles -> throws NETWORK_READBACK_MISMATCH -> reports FAILED
        storage.update {
            it.put("network_profiles", JSONObject())
        }
        assertFalse(engine.run())
        assertEquals("FAILED", report(storage).state)
        assertEquals("NETWORK_READBACK_MISMATCH", report(storage).reason)

        // Profile exists in network_profiles but not configured on platform -> throws NETWORK_READBACK_MISMATCH
        storage.update {
            it.put("network_profiles", JSONObject().put(prof.id, JSONObject().put("id", 99).put("profile", encode(prof))))
        }
        assertFalse(engine.run())
        assertEquals("FAILED", report(storage).state)
        assertEquals("NETWORK_READBACK_MISMATCH", report(storage).reason)

        // Probe fails -> reports PENDING, MANAGEMENT_UNREACHABLE, returns true
        storage.update {
            it.put("network_profiles", JSONObject().put(prof.id, profRecord))
        }
        val unreachPlatform = object : NetworkPlatform by platform {
            override fun probe(id: Int, policy: NetworkPolicy): Boolean = false
        }
        assertTrue(NetworkEngine(storage, unreachPlatform).run())
        assertEquals("PENDING", report(storage).state)
        assertEquals("MANAGEMENT_UNREACHABLE", report(storage).reason)

        // CA drift -> throws CA_DRIFT
        platform.certificates.clear()
        assertFalse(engine.run())
        assertEquals("FAILED", report(storage).state)
        assertEquals("CA_DRIFT", report(storage).reason)
    }

    @Test
    fun testValidationSecretsAndProfiles() {
        val platform = Platform()
        val storage = MemoryState(JSONObject())
        val engine = NetworkEngine(storage, platform)

        // OPEN profile with secret -> INVALID_PROFILE
        val openWithPass = NetworkPolicy.newBuilder()
            .setPrimaryId("p1")
            .addProfiles(WifiProfile.newBuilder().setId("p1").setSsid("OpenNet").setSecurity("OPEN").setPassphrase(com.google.protobuf.ByteString.copyFromUtf8("somepass")))
            .build()
        desired(storage, openWithPass, 20L)
        assertFalse(engine.run())
        assertEquals("INVALID_PROFILE", report(storage).reason)

        // WPA2_PSK with 64 hex characters -> valid
        val hex64 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
        val pskHex = NetworkPolicy.newBuilder()
            .setPrimaryId("p2")
            .addProfiles(WifiProfile.newBuilder().setId("p2").setSsid("HexNet").setSecurity("WPA2_PSK").setPassphrase(com.google.protobuf.ByteString.copyFromUtf8(hex64)))
            .build()
        desired(storage, pskHex, 21L)
        assertFalse(engine.run()) // Proceeds past validation, fails on recovery / probe
        assertNotEquals("INVALID_PROFILE", report(storage).reason)

        // WPA2_PSK with invalid length (< 8) -> INVALID_PROFILE
        val pskShort = NetworkPolicy.newBuilder()
            .setPrimaryId("p3")
            .addProfiles(WifiProfile.newBuilder().setId("p3").setSsid("ShortNet").setSecurity("WPA2_PSK").setPassphrase(com.google.protobuf.ByteString.copyFromUtf8("short")))
            .build()
        desired(storage, pskShort, 22L)
        assertFalse(engine.run())
        assertEquals("INVALID_PROFILE", report(storage).reason)

        // WPA3_SAE with control char -> INVALID_PROFILE
        val saeCtrl = NetworkPolicy.newBuilder()
            .setPrimaryId("p4")
            .addProfiles(WifiProfile.newBuilder().setId("p4").setSsid("SaeNet").setSecurity("WPA3_SAE").setPassphrase(com.google.protobuf.ByteString.copyFrom(byteArrayOf(10, 20, 30))))
            .build()
        desired(storage, saeCtrl, 23L)
        assertFalse(engine.run())
        assertEquals("INVALID_PROFILE", report(storage).reason)

        // WPA3_SAE with valid UTF-8 passphrase -> valid
        val saeValid = NetworkPolicy.newBuilder()
            .setPrimaryId("p5")
            .addProfiles(WifiProfile.newBuilder().setId("p5").setSsid("SaeValidNet").setSecurity("WPA3_SAE").setPassphrase(com.google.protobuf.ByteString.copyFromUtf8("validPassphrase123")))
            .build()
        desired(storage, saeValid, 24L)
        assertFalse(engine.run())
        assertNotEquals("INVALID_PROFILE", report(storage).reason)

        // Empty profiles with primary ID set -> PRIMARY_REQUIRED
        val emptyWithPrimary = NetworkPolicy.newBuilder().setPrimaryId("p_none").build()
        desired(storage, emptyWithPrimary, 25L)
        assertFalse(engine.run())
        assertEquals("PRIMARY_REQUIRED", report(storage).reason)

        // Duplicate profile SSID -> INVALID_PROFILE
        val dupSSID = NetworkPolicy.newBuilder()
            .setPrimaryId("p1")
            .addProfiles(WifiProfile.newBuilder().setId("p1").setSsid("SameSSID").setSecurity("OPEN"))
            .addProfiles(WifiProfile.newBuilder().setId("p2").setSsid("SameSSID").setSecurity("OPEN"))
            .build()
        desired(storage, dupSSID, 26L)
        assertFalse(engine.run())
        assertEquals("INVALID_PROFILE", report(storage).reason)

        // More than 8 profiles -> NETWORK_LIMIT
        val tooManyProfiles = NetworkPolicy.newBuilder().setPrimaryId("p0")
        for (i in 0..8) {
            tooManyProfiles.addProfiles(WifiProfile.newBuilder().setId("p$i").setSsid("SSID_$i").setSecurity("OPEN"))
        }
        desired(storage, tooManyProfiles.build(), 27L)
        assertFalse(engine.run())
        assertEquals("NETWORK_LIMIT", report(storage).reason)

        // Duplicate CA ID -> DUPLICATE_CA
        val ca1 = CACertificate.newBuilder().setId("ca-same").setDer(ByteString.copyFrom(caA)).build()
        val ca2 = CACertificate.newBuilder().setId("ca-same").setDer(ByteString.copyFrom(caB)).build()
        val dupCA = NetworkPolicy.newBuilder()
            .addAllCertificates(listOf(ca1, ca2))
            .build()
        desired(storage, dupCA, 28L)
        assertFalse(engine.run())
        assertEquals("DUPLICATE_CA", report(storage).reason)
    }
}
