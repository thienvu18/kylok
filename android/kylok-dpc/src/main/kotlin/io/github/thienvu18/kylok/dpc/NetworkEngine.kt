// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import io.github.thienvu18.kylok.proto.v1.*
import org.json.JSONArray
import org.json.JSONObject
import java.security.MessageDigest
import java.security.cert.CertificateFactory
import java.security.cert.X509Certificate

internal class NetworkRejected(val code: String) : Exception(code)
internal data class SavedNetwork(val id: Int, val ssid: String, val security: String)

/** Privileged network effects and network-bound authenticated management readback. */
internal interface NetworkPlatform {
    /** Check Device Owner and required runtime permissions before reading or changing network configuration. */
    fun check()
    /** Read the certificate validity clock. */
    fun now(): Long
    /** Read all saved configurations; absence of visibility must fail, not masquerade as an empty device. */
    fun configured(): List<SavedNetwork>
    /** Read only networks created by this host UID, for additional ownership checks. */
    fun callerConfigured(): List<SavedNetwork>
    /** Read the actually associated saved network ID, or -1 when unobservable/disconnected. */
    fun currentNetworkID(): Int
    /** Add/update the specified profile; updates require the exact caller-owned ID supplied by the journal. */
    fun upsert(profile: WifiProfile, id: Int?): Int
    /** Enable/select a saved route without removing any other configuration. */
    fun select(id: Int)
    /** Remove only the expected caller-owned network, after verified replacement connectivity. */
    fun remove(network: SavedNetwork)
    /** Read whether the exact CA is installed in Android's user trust store. */
    fun hasCA(der: ByteArray): Boolean
    /** Observe whether the host already trusts this CA before managed installation, independently of user-store ownership. */
    fun trustsCAByDefault(der: ByteArray): Boolean
    /** Install the exact CA, never its private key. */
    fun installCA(der: ByteArray)
    /** Remove only a CA whose absence at ownership capture was durably recorded. */
    fun removeCA(der: ByteArray)
    /** Authenticate a nonce-bound probe over this exact network with the proposed remaining trust, without redirects. */
    fun probe(id: Int, policy: NetworkPolicy): Boolean
}

/** Digest public CA bytes for ownership records without retaining a host business identifier. */
internal fun caDigest(der: ByteArray): String = MessageDigest.getInstance("SHA-256").digest(der).joinToString("") { "%02x".format(it) }

/** Require a valid bounded CA object; raw keys, leaf certificates and expired/future certificates are rejected. */
internal fun networkCertificate(der: ByteArray, now: Long): X509Certificate {
    if (der.isEmpty() || der.size > 16 * 1024) throw NetworkRejected("INVALID_CA")
    val stream = der.inputStream()
    val certificate = try { CertificateFactory.getInstance("X.509").generateCertificate(stream) as X509Certificate }
        catch (_: java.security.cert.CertificateException) { throw NetworkRejected("INVALID_CA") }
    // X.509 Key Usage bit 5 is keyCertSign; an absent optional extension permits CA signing.
    if (stream.available() != 0 || !certificate.encoded.contentEquals(der) || certificate.basicConstraints < 0 ||
        certificate.keyUsage?.let { it.getOrNull(5) != true } == true) throw NetworkRejected("INVALID_CA")
    try { certificate.checkValidity(java.util.Date(now)) }
    catch (_: java.security.cert.CertificateException) { throw NetworkRejected("CA_NOT_VALID") }
    return certificate
}

/** Reconcile saved Wi-Fi/private CAs with an independent recovery route and a pre-effect rollback journal. */
internal class NetworkEngine(private val storage: StateAccess, private val platform: NetworkPlatform) {
    companion object {
        // Keep removed-root distrust independent of asynchronous Android trust-store cache invalidation, with bounded storage.
        private const val MAXIMUM_RETIRED_CAS = 256
    }
    /** Read one immutable normalized network intent from the authenticated snapshot. */
    private fun desired(): DesiredNetworkPolicy? {
        val state = storage.read()
        if (!state.has("snapshot")) return null
        val response = SyncResponse.parseFrom(decode(state.getString("snapshot")))
        return if (response.hasDesiredNetworkPolicy()) response.desiredNetworkPolicy else null
    }

    /** Persist sanitized observed state; no SSID, passphrase, certificate bytes or transport credential is reported. */
    private fun report(want: DesiredNetworkPolicy, state: String, reason: String = "", verified: Boolean = false, cleanup: Boolean = false) {
        val value = NetworkReport.newBuilder().setRevision(want.revision).setState(state).setReason(reason)
            .setActiveProfileId(if (verified) want.policy.primaryId else "").setConnectivityVerified(verified).setCleanupComplete(cleanup).build()
        storage.update { it.put("network_report", encode(value)) }
    }

    /** Validate the complete replacement before touching any network or certificate. */
    private fun validate(policy: NetworkPolicy) {
        if (policy.profilesCount > 8 || policy.certificatesCount > 8) throw NetworkRejected("NETWORK_LIMIT")
        val ids = mutableSetOf<String>(); val ssids = mutableSetOf<String>(); val caIDs = mutableSetOf<String>(); val caDigests = mutableSetOf<String>()
        for (profile in policy.profilesList) {
            val pass = profile.passphrase.toByteArray()
            val validID = profile.id.matches(Regex("[A-Za-z][A-Za-z0-9_.-]{0,127}"))
            val validSSID = profile.ssid.toByteArray(Charsets.UTF_8).size in 1..32 && '\u0000' !in profile.ssid
            val printable = pass.all { (it.toInt() and 255) in 32..126 }
            val hexPSK = pass.size == 64 && pass.toString(Charsets.US_ASCII).matches(Regex("[0-9a-fA-F]{64}"))
            val validSecret = when (profile.security) {
                "OPEN" -> pass.isEmpty()
                "WPA2_PSK" -> pass.size in 8..63 && printable || hexPSK
                "WPA3_SAE" -> pass.size in 1..63 && pass.toString(Charsets.UTF_8).toByteArray().contentEquals(pass) && pass.none { (it.toInt() and 255) < 32 || it.toInt() == 127 }
                else -> false
            }
            if (!validID || !validSSID || !validSecret || !ids.add(profile.id) || !ssids.add(profile.ssid)) throw NetworkRejected("INVALID_PROFILE")
        }
        if (policy.profilesCount > 0 && policy.primaryId !in ids || policy.profilesCount == 0 && policy.primaryId.isNotEmpty()) throw NetworkRejected("PRIMARY_REQUIRED")
        for (ca in policy.certificatesList) {
            networkCertificate(ca.der.toByteArray(), platform.now())
            if (!ca.id.matches(Regex("[A-Za-z][A-Za-z0-9_.-]{0,127}")) || !caIDs.add(ca.id) || !caDigests.add(caDigest(ca.der.toByteArray()))) throw NetworkRejected("DUPLICATE_CA")
        }
    }

    /** Read the prior immutable policy, whose delivered credentials remain inside encrypted storage for rollback. */
    private fun goodPolicy(state: JSONObject): NetworkPolicy = if (state.has("network_current"))
        DesiredNetworkPolicy.parseFrom(decode(state.getString("network_current"))).policy else NetworkPolicy.getDefaultInstance()

    /** Verify a saved route's ID and identity before selecting it, including after reboot or user changes. */
    private fun route(record: JSONObject): SavedNetwork {
        val id = record.getInt("id")
        return platform.configured().firstOrNull { it.id == id && it.ssid == record.getString("ssid") && it.security == record.getString("security") }
            ?: throw NetworkRejected("RECOVERY_ROUTE_MISSING")
    }

    /** Capture one independent unmanaged connected route before the first managed effect; retain it through rotations. */
    private fun recovery(state: JSONObject): JSONObject {
        if (state.has("network_recovery")) { route(state.getJSONObject("network_recovery")); return state.getJSONObject("network_recovery") }
        val saved = platform.configured().firstOrNull { it.id == platform.currentNetworkID() } ?: throw NetworkRejected("RECOVERY_ROUTE_REQUIRED")
        if (state.objectValue("network_profiles").keys().asSequence().any { state.objectValue("network_profiles").getJSONObject(it).getInt("id") == saved.id }) throw NetworkRejected("INDEPENDENT_RECOVERY_REQUIRED")
        return JSONObject().put("id", saved.id).put("ssid", saved.ssid).put("security", saved.security)
    }

    /** Select and authenticate the exact saved route; association or access to a different network cannot satisfy this check. */
    private fun connect(id: Int, policy: NetworkPolicy) {
        if (platform.currentNetworkID() != id) platform.select(id)
        if (!platform.probe(id, policy)) throw NetworkRejected("MANAGEMENT_UNREACHABLE")
    }

    /** Check both the journal and Android creator list before updating or deleting a managed network. */
    private fun owned(record: JSONObject): SavedNetwork? {
        val profile = WifiProfile.parseFrom(decode(record.getString("profile")))
        return platform.callerConfigured().firstOrNull { it.id == record.getInt("id") && it.ssid == profile.ssid && it.security == profile.security }
    }

    /** Stage one profile, recording its intent and initial IDs before Android can create/update its saved configuration. */
    private fun stage(profile: WifiProfile) {
        val state = storage.read()
        val prior = state.objectValue("network_profiles")
        val match = prior.keys().asSequence().map { prior.getJSONObject(it) }.firstOrNull {
            WifiProfile.parseFrom(decode(it.getString("profile"))).ssid == profile.ssid
        }
        val existing = platform.configured().filter { it.ssid == profile.ssid }
        val expected = match?.let { owned(it) ?: throw NetworkRejected("NETWORK_OWNERSHIP_CHANGED") }
        if (existing.any { it.id != expected?.id }) throw NetworkRejected("UNMANAGED_SSID_COLLISION")
        val before = JSONArray(platform.callerConfigured().map { it.id })
        storage.update { current -> current.getJSONObject("network_transaction").put("pending_profile", JSONObject()
            .put("profile", encode(profile)).put("before", before).put("expected", expected?.id ?: -1)) }
        val id = platform.upsert(profile, expected?.id)
        val observed = platform.callerConfigured().firstOrNull { it.id == id && it.ssid == profile.ssid && it.security == profile.security }
            ?: throw NetworkRejected("NETWORK_READBACK_MISMATCH")
        storage.update { current -> current.getJSONObject("network_transaction").objectValue("profiles")
            .put(profile.id, JSONObject().put("id", observed.id).put("profile", encode(profile)))
            current.getJSONObject("network_transaction").remove("pending_profile") }
    }

    /** Recover a network-creation effect whose return value was lost, without claiming pre-existing or unrelated host networks. */
    private fun recoverCreation() {
        val state = storage.read(); val transaction = state.getJSONObject("network_transaction")
        val pending = transaction.optJSONObject("pending_profile") ?: return
        val profile = WifiProfile.parseFrom(decode(pending.getString("profile")))
        val before = pending.getJSONArray("before")
        val existing = (0 until before.length()).map { before.getInt(it) }.toSet()
        val created = platform.callerConfigured().filter { it.id !in existing && it.ssid == profile.ssid && it.security == profile.security }
        if (created.size > 1) throw NetworkRejected("NETWORK_CREATION_UNCERTAIN")
        if (pending.getInt("expected") >= 0) {
            val updated = platform.callerConfigured().firstOrNull { it.id == pending.getInt("expected") && it.ssid == profile.ssid && it.security == profile.security }
            if (updated != null) storage.update { current -> current.getJSONObject("network_transaction").objectValue("profiles")
                .put(profile.id, JSONObject().put("id", updated.id).put("profile", encode(profile))) }
        }
        if (created.size == 1) storage.update { current -> current.getJSONObject("network_transaction").objectValue("profiles")
            .put(profile.id, JSONObject().put("id", created.single().id).put("profile", encode(profile))) }
    }

    /** Add trust only after journaling whether the exact CA pre-existed; unrelated trust is never removed. */
    private fun stageCA(ca: CACertificate) {
        val der = ca.der.toByteArray(); val digest = caDigest(der)
        if (!storage.read().objectValue("network_cas").has(digest)) {
            val baseline = platform.hasCA(der)
            val retired = storage.read().objectValue("network_untrusted_cas")
            val journal = storage.read().objectValue("network_cas")
            val trustBaseline = !retired.has(digest) && platform.trustsCAByDefault(der)
            val known = retired.keys().asSequence().toSet() + journal.keys().asSequence().filter { !journal.getJSONObject(it).getBoolean("trust_baseline") }.toSet() + digest
            if (!trustBaseline && known.size > MAXIMUM_RETIRED_CAS) throw NetworkRejected("TRUST_HISTORY_LIMIT")
            storage.update { it.objectValue("network_cas").put(digest, JSONObject().put("der", java.util.Base64.getEncoder().encodeToString(der)).put("baseline", baseline).put("trust_baseline", trustBaseline)) }
        }
        if (!platform.hasCA(der)) platform.installCA(der)
        if (!platform.hasCA(der)) throw NetworkRejected("CA_READBACK_MISMATCH")
    }

    /** Remove only journaled CAs created by Kylok after a successful probe with the proposed remaining trust. */
    private fun cleanupCAs(policy: NetworkPolicy, verifiedNetwork: Int) {
        val retain = policy.certificatesList.map { caDigest(it.der.toByteArray()) }.toSet()
        val journal = storage.read().objectValue("network_cas")
        for (digest in journal.keys().asSequence().toList()) {
            if (digest in retain) continue
            val record = journal.getJSONObject(digest)
            val der = java.util.Base64.getDecoder().decode(record.getString("der"))
            if (!record.getBoolean("baseline") && platform.hasCA(der)) { verifyCleanupRoute(verifiedNetwork, policy); platform.removeCA(der) }
            if (!record.getBoolean("baseline") && platform.hasCA(der)) throw NetworkRejected("CA_RELEASE_PENDING")
            storage.update {
                if (!record.getBoolean("trust_baseline")) it.objectValue("network_untrusted_cas").put(digest, true)
                it.objectValue("network_cas").remove(digest)
            }
        }
    }

    /** Recheck the replacement immediately before destructive cleanup; an unobservable/switched route cannot authorize it. */
    private fun verifyCleanupRoute(id: Int, policy: NetworkPolicy) {
        if (platform.currentNetworkID() != id || !platform.probe(id, policy)) throw NetworkRejected("CONNECTIVITY_CHANGED")
    }

    /** Complete only owned-resource cleanup after the caller has reverified replacement connectivity. */
    private fun finalize(want: DesiredNetworkPolicy, before: JSONObject) {
        val staged = storage.read().getJSONObject("network_transaction").getJSONObject("profiles")
        val target = staged.optJSONObject(want.policy.primaryId)?.getInt("id") ?: route(storage.read().getJSONObject("network_recovery")).id
        val retained = staged.keys().asSequence().map { staged.getJSONObject(it).getInt("id") }.toSet() + target
        for (key in before.keys()) if (before.getJSONObject(key).getInt("id") !in retained) owned(before.getJSONObject(key))?.let { verifyCleanupRoute(target, want.policy); platform.remove(it) }
        cleanupCAs(want.policy, target)
        storage.update { current -> current.put("network_profiles", staged).put("network_current", encode(want)).remove("network_failed_revision"); current.remove("network_transaction")
            if (want.policy.profilesCount == 0 && want.policy.certificatesCount == 0) current.remove("network_recovery") }
    }

    /** Restore known managed credentials on the independent recovery route; leave effects journaled until safe cleanup is proven. */
    private fun rollback() {
        platform.check(); recoverCreation()
        val state = storage.read(); val transaction = state.getJSONObject("network_transaction")
        val previous = goodPolicy(state)
        val recovery = route(state.getJSONObject("network_recovery"))
        // Recovery trust is constructed from the prior document, so it does not rely on any newly staged CA.
        connect(recovery.id, previous)
        for (ca in previous.certificatesList) stageCA(ca)
        val restored = JSONObject()
        val old = transaction.getJSONObject("before_profiles")
        for (key in old.keys()) {
            val record = old.getJSONObject(key)
            val profile = WifiProfile.parseFrom(decode(record.getString("profile")))
            val staged = transaction.objectValue("profiles")
            val changedByTransaction = staged.keys().asSequence().map { staged.getJSONObject(it) }.firstOrNull { it.getInt("id") == record.getInt("id") }
            val prior = owned(record) ?: changedByTransaction?.let { owned(it) }
            for (collision in platform.configured().filter { it.ssid == profile.ssid && it.id != prior?.id }) {
                val known = staged.keys().asSequence().map { staged.getJSONObject(it) }.any { owned(it) == collision }
                if (!known) throw NetworkRejected("NETWORK_OWNERSHIP_CHANGED")
                verifyCleanupRoute(recovery.id, previous); platform.remove(collision)
            }
            val id = platform.upsert(profile, prior?.id)
            restored.put(key, JSONObject().put("id", id).put("profile", encode(profile)))
        }
        // Never remove the selected recovery route, and retain restored configurations even if their IDs changed.
        val retained = restored.keys().asSequence().map { restored.getJSONObject(it).getInt("id") }.toSet() + recovery.id
        val staged = transaction.objectValue("profiles")
        for (key in staged.keys()) {
            val record = staged.getJSONObject(key)
            if (record.getInt("id") in retained) continue
            owned(record)?.let { verifyCleanupRoute(recovery.id, previous); platform.remove(it) }
        }
        cleanupCAs(previous, recovery.id)
        storage.update { it.put("network_profiles", restored).remove("network_transaction") }
    }

    /** Recover an interrupted migration before opening the normal sync connection; never repeat its unfinished intent. */
    fun recover(): Boolean {
        val state = storage.read()
        if (!state.has("network_transaction")) return false
        val intent = DesiredNetworkPolicy.parseFrom(decode(state.getJSONObject("network_transaction").getString("intent")))
        return try {
            val transaction = storage.read().getJSONObject("network_transaction")
            if (transaction.optBoolean("verified")) {
                platform.check(); validate(intent.policy)
                val profiles = transaction.objectValue("profiles")
                if (intent.policy.profilesList.any { profile -> profiles.optJSONObject(profile.id)?.let { owned(it) } == null }) throw NetworkRejected("NETWORK_READBACK_MISMATCH")
                val target = profiles.optJSONObject(intent.policy.primaryId)?.getInt("id") ?: route(storage.read().getJSONObject("network_recovery")).id
                connect(target, intent.policy)
                finalize(intent, transaction.getJSONObject("before_profiles"))
                report(intent, "APPLIED", verified = true, cleanup = true)
                return false
            }
            rollback()
            storage.update { it.put("network_failed_revision", intent.revision) }
            report(intent, "FAILED", "INTERRUPTED_MIGRATION_RECOVERED"); false
        } catch (_: NetworkRejected) { report(intent, "PENDING", "RECOVERY_PENDING"); true }
        catch (_: SecurityException) { report(intent, "PENDING", "RECOVERY_PERMISSION_REQUIRED"); true }
    }

    /** Recover management connectivity after a committed primary route fails; retain every managed setting for an explicit host retry. */
    fun recoverConnectivity(): Boolean {
        val state = storage.read()
        if (state.has("network_transaction") || !state.has("network_current") || !state.has("network_recovery")) return false
        val current = DesiredNetworkPolicy.parseFrom(decode(state.getString("network_current")))
        if (current.policy.profilesCount == 0) return false
        return try {
            platform.check()
            val primary = state.objectValue("network_profiles").optJSONObject(current.policy.primaryId)?.let { owned(it) }
            if (state.optLong("network_failed_revision") != current.revision && primary != null && platform.currentNetworkID() == primary.id && platform.probe(primary.id, current.policy)) return false
            connect(route(state.getJSONObject("network_recovery")).id, current.policy)
            storage.update { it.put("network_failed_revision", current.revision) }
            report(current, "FAILED", "RECOVERY_ROUTE_ACTIVE"); false
        } catch (_: NetworkRejected) { report(current, "PENDING", "RECOVERY_PENDING"); true }
        catch (_: SecurityException) { report(current, "UNSUPPORTED", "NETWORK_PERMISSION_REQUIRED"); false }
    }

    /** Apply an authenticated replacement, verifying post-change connectivity before releasing owned resources. */
    fun run(): Boolean {
        if (storage.read().has("network_transaction")) return recover()
        val want = desired() ?: return false
        val state = storage.read()
        if (state.optLong("network_failed_revision") == want.revision) return false
        try {
            platform.check(); validate(want.policy)
            val savedRecovery = recovery(state)
            if (want.policy.profilesList.any { it.ssid == savedRecovery.getString("ssid") }) throw NetworkRejected("INDEPENDENT_RECOVERY_REQUIRED")
            if (state.has("network_current") && DesiredNetworkPolicy.parseFrom(decode(state.getString("network_current"))) == want) {
                if (want.policy.profilesList.any { profile -> state.objectValue("network_profiles").optJSONObject(profile.id)?.let { owned(it) } == null }) throw NetworkRejected("NETWORK_READBACK_MISMATCH")
                val primary = state.objectValue("network_profiles").optJSONObject(want.policy.primaryId)
                val target = primary?.let { owned(it)?.id ?: throw NetworkRejected("NETWORK_OWNERSHIP_CHANGED") } ?: route(savedRecovery).id
                if (platform.currentNetworkID() != target || !platform.probe(target, want.policy)) {
                    report(want, "PENDING", "MANAGEMENT_UNREACHABLE"); return true
                }
                if (want.policy.certificatesList.any { !platform.hasCA(it.der.toByteArray()) }) throw NetworkRejected("CA_DRIFT")
                report(want, "APPLIED", verified = true, cleanup = true); return false
            }
            // Prove the recovery path before a transaction can modify a managed credential or trust entry.
            connect(route(savedRecovery).id, goodPolicy(state))
            storage.update { current -> current.put("network_recovery", savedRecovery)
                .put("network_transaction", JSONObject().put("intent", encode(want))
                    .put("before_profiles", JSONObject(current.objectValue("network_profiles").toString())).put("profiles", JSONObject())) }
            for (ca in want.policy.certificatesList) stageCA(ca)
            for (profile in want.policy.profilesList) stage(profile)
            val staged = storage.read().getJSONObject("network_transaction").getJSONObject("profiles")
            val target = staged.optJSONObject(want.policy.primaryId)?.getInt("id") ?: route(savedRecovery).id
            connect(target, want.policy)
            // Persist verified replacement before any cleanup; recovery can reconstruct prior profiles if cleanup is interrupted.
            storage.update { it.getJSONObject("network_transaction").put("verified", true) }
            finalize(want, state.objectValue("network_profiles"))
            report(want, "APPLIED", verified = true, cleanup = true); return false
        } catch (error: NetworkRejected) {
            if (storage.read().has("network_transaction")) {
                try { rollback(); storage.update { it.put("network_failed_revision", want.revision) } }
                catch (_: NetworkRejected) { report(want, "PENDING", "RECOVERY_PENDING"); return true }
                catch (_: SecurityException) { report(want, "PENDING", "RECOVERY_PERMISSION_REQUIRED"); return true }
            }
            report(want, "FAILED", error.code); return false
        } catch (_: SecurityException) { report(want, "UNSUPPORTED", "NETWORK_PERMISSION_REQUIRED"); return storage.read().has("network_transaction") }
    }
}
