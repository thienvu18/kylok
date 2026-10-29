// SPDX-License-Identifier: LGPL-3.0-only
package android.net.wifi

/** Test double for WifiInfo. */
class TestWifiInfo(var netId: Int = 1, var secType: Int = WifiInfo.SECURITY_TYPE_PSK) : WifiInfo() {
    override fun getNetworkId(): Int = netId
    override fun getCurrentSecurityType(): Int = secType
}

/** Test double for WifiManager. */
@Suppress("DEPRECATION")
class TestWifiManager : WifiManager() {
    var wifiEnabled: Boolean = true
    var wpa3Supported: Boolean = true
    val savedConfigs = mutableListOf<WifiConfiguration>()
    val callerConfigs = mutableListOf<WifiConfiguration>()
    var nextNetId = 1
    var connInfo: WifiInfo = TestWifiInfo()
    var enableNetworkResult: Boolean = true
    var removeNetworkResult: Boolean = true
    var returnNullConfiguredNetworks: Boolean = false
    var updateNetworkResult: Int? = null

    override fun isWifiEnabled(): Boolean = wifiEnabled
    override fun isWpa3SaeSupported(): Boolean = wpa3Supported
    override fun getConfiguredNetworks(): MutableList<WifiConfiguration>? =
        if (returnNullConfiguredNetworks) null else savedConfigs
    override fun getCallerConfiguredNetworks(): MutableList<WifiConfiguration> = callerConfigs
    override fun getConnectionInfo(): WifiInfo = connInfo

    override fun addNetworkPrivileged(config: WifiConfiguration): AddNetworkResult {
        val id = nextNetId++
        config.networkId = id
        savedConfigs.add(config)
        callerConfigs.add(config)
        return AddNetworkResult(AddNetworkResult.STATUS_SUCCESS, id)
    }

    override fun updateNetwork(config: WifiConfiguration): Int {
        if (updateNetworkResult != null) return updateNetworkResult!!
        val id = config.networkId
        val idx = savedConfigs.indexOfFirst { it.networkId == id }
        if (idx >= 0) savedConfigs[idx] = config
        val cIdx = callerConfigs.indexOfFirst { it.networkId == id }
        if (cIdx >= 0) callerConfigs[cIdx] = config
        return id
    }

    override fun enableNetwork(netId: Int, attemptConnect: Boolean): Boolean = enableNetworkResult

    override fun removeNetwork(netId: Int): Boolean {
        if (!removeNetworkResult) return false
        savedConfigs.removeIf { it.networkId == netId }
        callerConfigs.removeIf { it.networkId == netId }
        return true
    }
}
