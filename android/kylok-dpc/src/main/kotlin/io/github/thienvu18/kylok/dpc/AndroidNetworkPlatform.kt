// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.Manifest
import android.app.admin.DevicePolicyManager
import android.content.ComponentName
import android.content.Context
import android.content.pm.PackageManager
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.wifi.WifiConfiguration
import android.net.wifi.WifiInfo
import android.net.wifi.WifiManager
import android.os.SystemClock
import io.github.thienvu18.kylok.proto.v1.NetworkPolicy
import io.github.thienvu18.kylok.proto.v1.WifiProfile
import java.net.HttpURLConnection
import java.net.URL
import java.security.SecureRandom
import java.util.Base64
import javax.net.ssl.HttpsURLConnection

/** Device Owner Wi-Fi and user-CA adapter; management traffic is verified on the specifically selected Android Network. */
@Suppress("DEPRECATION") // Saved-network modification is explicitly supported for Device Owner despite ordinary-app deprecation.
internal class AndroidNetworkPlatform(private val context: Context, private val storage: StateAccess) : NetworkPlatform {
    private val wifi = (context.getSystemService(WifiManager::class.java) ?: context.getSystemService(Context.WIFI_SERVICE)) as WifiManager
    private val connectivity = (context.getSystemService(ConnectivityManager::class.java) ?: context.getSystemService(Context.CONNECTIVITY_SERVICE)) as ConnectivityManager
    private val dpm = (context.getSystemService(DevicePolicyManager::class.java) ?: context.getSystemService(Context.DEVICE_POLICY_SERVICE)) as DevicePolicyManager
    private val admin = ComponentName(context, KylokDeviceAdminReceiver::class.java)
    companion object {
        private const val ASSOCIATION_TIMEOUT_MILLIS = 15_000L
        private const val ASSOCIATION_POLL_MILLIS = 200L
        private const val PROBE_TIMEOUT_MILLIS = 5_000
        val CAPABILITIES = listOf("network.lifecycle")
    }
    /** Require ownership and host-granted observation permission before making assumptions about configured networks. */
    override fun check() {
        if (wifi == null || connectivity == null) throw NetworkRejected("WIFI_UNSUPPORTED")
        if (!dpm.isDeviceOwnerApp(context.packageName)) throw NetworkRejected("DEVICE_OWNER_REQUIRED")
        if (context.checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION) != PackageManager.PERMISSION_GRANTED ||
            context.checkSelfPermission(Manifest.permission.NEARBY_WIFI_DEVICES) != PackageManager.PERMISSION_GRANTED) throw NetworkRejected("NETWORK_PERMISSION_REQUIRED")
        if (!wifi.isWifiEnabled) throw NetworkRejected("WIFI_DISABLED")
    }
    /** Use wall time only for certificate validity, never for association timeout measurement. */
    override fun now() = System.currentTimeMillis()
    /** Classify exactly the supported security types; unknown configurations cannot be mistaken for managed profiles. */
    private fun security(config: WifiConfiguration): String = when {
        config.allowedKeyManagement?.get(WifiConfiguration.KeyMgmt.SAE) == true -> "WPA3_SAE"
        config.allowedKeyManagement?.get(WifiConfiguration.KeyMgmt.WPA_PSK) == true -> "WPA2_PSK"
        config.allowedKeyManagement?.get(WifiConfiguration.KeyMgmt.NONE) == true && (config.wepKeys?.all { it == null } != false) -> "OPEN"
        else -> "OTHER"
    }
    /** Decode literal/hexadecimal Android SSID bytes without interpreting host values as JSON escapes. */
    private fun ssid(config: WifiConfiguration): String = NetworkEncoding.ssid(config.SSID ?: throw NetworkRejected("SSID_UNOBSERVABLE"))
    /** Convert only supported visible fields; unrelated configurations are retained verbatim by Android. */
    private fun saved(config: WifiConfiguration) = SavedNetwork(config.networkId, ssid(config), security(config))
    /** Full visibility is needed to reject collisions with unmanaged SSIDs. */
    override fun configured(): List<SavedNetwork> {
        if (context.checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION) != PackageManager.PERMISSION_GRANTED) throw NetworkRejected("NETWORK_PERMISSION_REQUIRED")
        return wifi.configuredNetworks?.map { saved(it) } ?: throw NetworkRejected("NETWORKS_UNOBSERVABLE")
    }
    /** A matching SSID alone cannot authorize deletion of a network created by another application. */
    override fun callerConfigured(): List<SavedNetwork> = wifi.callerConfiguredNetworks.map { saved(it) }
    /** A redacted/absent network ID cannot satisfy a migration check. */
    override fun currentNetworkID() = wifi?.connectionInfo?.networkId ?: -1
    /** Quote literal values for Android's saved-network configuration format. */
    private fun quoted(value: String) = NetworkEncoding.quoted(value)
    /** Add a new disabled configuration or update the exact verified caller-owned network. */
    override fun upsert(profile: WifiProfile, id: Int?): Int {
        check()
        if (id != null && callerConfigured().none { it.id == id && it.ssid == profile.ssid }) throw NetworkRejected("NETWORK_OWNERSHIP_CHANGED")
        if (id == null && configured().any { it.ssid == profile.ssid }) throw NetworkRejected("UNMANAGED_SSID_COLLISION")
        val config = WifiConfiguration().apply {
            SSID = quoted(profile.ssid); hiddenSSID = profile.hidden
            when (profile.security) {
                "OPEN" -> setSecurityParams(WifiConfiguration.SECURITY_TYPE_OPEN)
                "WPA2_PSK" -> {
                    setSecurityParams(WifiConfiguration.SECURITY_TYPE_PSK)
                    // Require RSN/AES for the explicit WPA2 security type.
                    (allowedProtocols ?: java.util.BitSet().also { allowedProtocols = it }).apply { clear(); set(WifiConfiguration.Protocol.RSN) }
                    (allowedPairwiseCiphers ?: java.util.BitSet().also { allowedPairwiseCiphers = it }).apply { clear(); set(WifiConfiguration.PairwiseCipher.CCMP) }
                    (allowedGroupCiphers ?: java.util.BitSet().also { allowedGroupCiphers = it }).apply { clear(); set(WifiConfiguration.GroupCipher.CCMP) }
                }
                "WPA3_SAE" -> {
                    if (!wifi.isWpa3SaeSupported) throw NetworkRejected("WPA3_UNSUPPORTED")
                    setSecurityParams(WifiConfiguration.SECURITY_TYPE_SAE)
                }
                else -> throw NetworkRejected("SECURITY_UNSUPPORTED")
            }
            if (profile.security != "OPEN") {
                val value = profile.passphrase.toStringUtf8()
                preSharedKey = if (profile.security == "WPA2_PSK" && value.matches(Regex("[0-9a-fA-F]{64}"))) value else quoted(value)
            }
        }
        val result = if (id == null) wifi.addNetworkPrivileged(config).networkId else {
            config.networkId = id; wifi.updateNetwork(config)
        }
        if (result < 0 || id != null && result != id) throw NetworkRejected("NETWORK_WRITE_REJECTED")
        // False only enables the new network; true is reserved for the subsequent explicit selection/probe.
        if (!wifi.enableNetwork(result, false)) throw NetworkRejected("NETWORK_ENABLE_REJECTED")
        return result
    }
    /** Select a known recovery/managed route; keep every saved configuration intact. */
    override fun select(id: Int) {
        check()
        if (configured().none { it.id == id } || !wifi.enableNetwork(id, true)) throw NetworkRejected("NETWORK_SELECTION_REJECTED")
    }
    /** Recheck creator, ID, SSID and security immediately before a managed network removal. */
    override fun remove(network: SavedNetwork) {
        check()
        if (callerConfigured().none { it == network }) throw NetworkRejected("NETWORK_OWNERSHIP_CHANGED")
        if (currentNetworkID() < 0) throw NetworkRejected("ACTIVE_ROUTE_UNOBSERVABLE")
        if (currentNetworkID() == network.id) throw NetworkRejected("ACTIVE_ROUTE_REMOVAL_BLOCKED")
        if (!wifi.removeNetwork(network.id) || configured().any { it.id == network.id }) throw NetworkRejected("NETWORK_RELEASE_PENDING")
    }
    /** Observe exact certificate bytes rather than an installation setter's result. */
    override fun hasCA(der: ByteArray) = dpm.hasCaCertInstalled(admin, der)
    /** Preserve an existing APK/base trust anchor even when Kylok temporarily installs the same CA in user storage. */
    override fun trustsCAByDefault(der: ByteArray) = NetworkTrust.baselineContains(der)
    /** Install an already validated CA through Device Owner privileges. */
    override fun installCA(der: ByteArray) { if (!dpm.installCaCert(admin, der)) throw NetworkRejected("CA_INSTALL_REJECTED") }
    /** Caller ownership is checked by the durable engine journal before this exact-byte removal. */
    override fun removeCA(der: ByteArray) = dpm.uninstallCaCert(admin, der)

    /** Resolve an associated Wi-Fi Network without substituting cellular/default-network reachability. */
    fun selectedNetwork(id: Int = currentNetworkID()): Network? {
        if (id < 0 || currentNetworkID() != id) return null
        val networks = connectivity.allNetworks.filter { connectivity.getNetworkCapabilities(it)?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) == true }
        val exact = networks.firstOrNull { (connectivity.getNetworkCapabilities(it)?.transportInfo as? WifiInfo)?.networkId == id }
        return exact ?: networks.singleOrNull()
    }
    /** Use current managed trust and Wi-Fi routing for same-origin APK downloads without weakening hostname verification. */
    fun openConnection(url: URL): HttpURLConnection {
        val state = storage.read()
        val network = if (state.has("network_current") || state.has("network_transaction")) selectedNetwork() else null
        val connection = (network?.openConnection(url) ?: url.openConnection()) as HttpURLConnection
        if (connection is HttpsURLConnection) NetworkTrust.factory(state)?.let { connection.sslSocketFactory = it }
        return connection
    }
    /** Authenticate a fresh nonce over the selected Wi-Fi route; TLS chain and hostname checks remain enabled. */
    override fun probe(id: Int, policy: NetworkPolicy): Boolean {
        val until = SystemClock.elapsedRealtime() + ASSOCIATION_TIMEOUT_MILLIS
        var network = selectedNetwork(id)
        while (network == null && SystemClock.elapsedRealtime() < until) {
            if (Thread.currentThread().isInterrupted) throw java.io.InterruptedIOException("Network probe interrupted")
            Thread.sleep(ASSOCIATION_POLL_MILLIS); network = selectedNetwork(id)
        }
        val selected = network ?: return false
        val observedSSID = configured().firstOrNull { it.id == id }?.ssid
        val managed = policy.profilesList.firstOrNull { it.ssid == observedSSID }
        if (managed != null) {
            val requiredSecurity = when (managed.security) {
                "OPEN" -> WifiInfo.SECURITY_TYPE_OPEN
                "WPA2_PSK" -> WifiInfo.SECURITY_TYPE_PSK
                "WPA3_SAE" -> WifiInfo.SECURITY_TYPE_SAE
                else -> return false
            }
            if (wifi.connectionInfo.currentSecurityType != requiredSecurity) return false
        }
        val state = storage.read()
        val nonce = Base64.getUrlEncoder().withoutPadding().encodeToString(ByteArray(32).also { SecureRandom().nextBytes(it) })
        val connection = selected.openConnection(URL(state.getString("server") + "/network-probe")) as HttpURLConnection
        try {
            connection.instanceFollowRedirects = false
            connection.connectTimeout = PROBE_TIMEOUT_MILLIS; connection.readTimeout = PROBE_TIMEOUT_MILLIS
            if (connection is HttpsURLConnection) NetworkTrust.factory(state, policy)?.let { connection.sslSocketFactory = it }
            connection.setRequestProperty("Authorization", "Bearer ${state.getString("credential")}")
            connection.setRequestProperty("X-Kylok-Device-ID", state.getString("device_id"))
            connection.setRequestProperty("X-Kylok-Probe", nonce)
            return connection.responseCode == HttpURLConnection.HTTP_NO_CONTENT && connection.getHeaderField("X-Kylok-Probe") == nonce && currentNetworkID() == id
        } catch (_: java.io.IOException) { return false }
        finally { connection.disconnect() }
    }
}
