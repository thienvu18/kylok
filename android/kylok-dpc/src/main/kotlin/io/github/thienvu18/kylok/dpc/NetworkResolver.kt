// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.net.Network
import io.grpc.EquivalentAddressGroup
import io.grpc.NameResolver
import io.grpc.Status
import java.net.InetSocketAddress
import java.net.URI
import java.util.concurrent.atomic.AtomicBoolean

/** Resolve LAN hostnames on the selected Wi-Fi network rather than a cellular/default DNS route. */
@Suppress("DEPRECATION") // A per-channel factory keeps this resolver isolated from the host's process-wide registry.
internal class NetworkResolver(private val network: Network, private val host: String, private val port: Int) : NameResolver.Factory() {
    /** Match the channel's DNS target; the verified origin supplies the exact immutable host/port. */
    override fun newNameResolver(targetUri: URI, args: NameResolver.Args): NameResolver? {
        if (targetUri.scheme != "dns") return null
        return object : NameResolver() {
            private val active = AtomicBoolean(true)
            /** Keep TLS authority equal to the enrolled origin, including IPv6 literals. */
            override fun getServiceAuthority() = originAuthority(host, port)
            /** Resolve off the gRPC synchronization thread and suppress delivery after channel shutdown. */
            override fun start(listener: Listener2) {
                (args.offloadExecutor ?: java.util.concurrent.ForkJoinPool.commonPool()).execute {
                    try {
                        val addresses = network.getAllByName(host).map { EquivalentAddressGroup(InetSocketAddress(it, port)) }
                        if (active.get()) listener.onResult(ResolutionResult.newBuilder().setAddresses(addresses).build())
                    } catch (_: Exception) { if (active.get()) listener.onError(Status.UNAVAILABLE.withDescription("Selected-network DNS unavailable")) }
                }
            }
            /** A closed channel must not receive an in-flight lookup result. */
            override fun shutdown() { active.set(false) }
        }
    }
    /** Keep standard DNS target parsing without installing a global resolver. */
    override fun getDefaultScheme() = "dns"
}
