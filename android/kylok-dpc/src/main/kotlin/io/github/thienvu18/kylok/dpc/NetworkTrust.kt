// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import io.github.thienvu18.kylok.proto.v1.DesiredNetworkPolicy
import io.github.thienvu18.kylok.proto.v1.NetworkPolicy
import org.json.JSONObject
import java.security.KeyStore
import javax.net.ssl.SSLContext
import javax.net.ssl.SSLSocketFactory
import javax.net.ssl.TrustManagerFactory
import javax.net.ssl.X509TrustManager

/** Management TLS trust remains origin/hostname-verified and independent from bootstrap APK trust. */
internal object NetworkTrust {
    /** Read the host's default base anchors before installing managed user certificates. */
    private fun defaults(): X509TrustManager = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm()).apply { init(null as KeyStore?) }
        .trustManagers.filterIsInstance<X509TrustManager>().single()
    /** Distinguish host-provided bootstrap/base trust from a newly managed user-store anchor. */
    fun baselineContains(der: ByteArray): Boolean = defaults().acceptedIssuers.any { it.encoded.contentEquals(der) }

    /** Build baseline plus exactly proposed private trust, excluding CAs journaled as newly owned and omitted from the proposal. */
    fun factory(state: JSONObject, proposal: NetworkPolicy? = null): SSLSocketFactory? {
        val journal = state.objectValue("network_cas")
        val policies = mutableListOf<NetworkPolicy>()
        if (proposal != null) policies += proposal else {
            if (state.has("network_current")) policies += DesiredNetworkPolicy.parseFrom(decode(state.getString("network_current"))).policy
            // During recovery, either authenticated side of a staged CA rotation can carry the next sync.
            state.optJSONObject("network_transaction")?.let { policies += DesiredNetworkPolicy.parseFrom(decode(it.getString("intent"))).policy }
        }
        val retired = state.objectValue("network_untrusted_cas")
        if (journal.length() == 0 && retired.length() == 0 && policies.all { it.certificatesCount == 0 }) return null
        // The proposal/pending intent is authoritative for reintroducing a retired root; the rollback policy cannot reauthorize it.
        val allowedRetired = policies.lastOrNull()?.certificatesList?.map { caDigest(it.der.toByteArray()) }?.toSet() ?: emptySet()
        val excluded = journal.keys().asSequence().filter { !journal.getJSONObject(it).getBoolean("trust_baseline") }.toSet() + retired.keys().asSequence().toSet()
        val defaults = defaults()
        val roots = KeyStore.getInstance(KeyStore.getDefaultType()).apply { load(null, null) }
        for ((index, certificate) in defaults.acceptedIssuers.withIndex()) {
            if (caDigest(certificate.encoded) !in excluded) roots.setCertificateEntry("baseline-$index", certificate)
        }
        for (policy in policies) for (ca in policy.certificatesList) {
            if (retired.has(caDigest(ca.der.toByteArray())) && caDigest(ca.der.toByteArray()) !in allowedRetired) continue
            // Expired desired trust cannot prevent the agent fetching a replacement over another valid trust path.
            val certificate = try { networkCertificate(ca.der.toByteArray(), System.currentTimeMillis()) }
                catch (error: NetworkRejected) { if (proposal != null) throw error else continue }
            roots.setCertificateEntry("managed-${caDigest(ca.der.toByteArray())}", certificate)
        }
        val managers = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm()).apply { init(roots) }
        return SSLContext.getInstance("TLS").apply { init(null, managers.trustManagers, null) }.socketFactory
    }
}
