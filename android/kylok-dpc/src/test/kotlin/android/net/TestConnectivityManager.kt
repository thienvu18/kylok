// SPDX-License-Identifier: LGPL-3.0-only
package android.net

import java.net.URL
import java.net.URLConnection

/** Test double for Network. */
class TestNetwork(private val factory: (URL) -> URLConnection = { it.openConnection() }) : Network() {
    var allByName: (String) -> Array<java.net.InetAddress> = { host -> arrayOf(java.net.InetAddress.getByName(host)) }
    override fun openConnection(url: URL): URLConnection = factory(url)
    override fun getAllByName(host: String): Array<java.net.InetAddress> = allByName(host)
    override fun getSocketFactory(): javax.net.SocketFactory = javax.net.SocketFactory.getDefault()
}

/** Test double for ConnectivityManager. */
class TestConnectivityManager : ConnectivityManager() {
    val networkList = mutableListOf<Network>()
    val capabilitiesMap = mutableMapOf<Network, NetworkCapabilities>()

    override fun getAllNetworks(): Array<Network> = networkList.toTypedArray()
    override fun getNetworkCapabilities(network: Network?): NetworkCapabilities? =
        capabilitiesMap[network]
}
