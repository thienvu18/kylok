// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import com.google.protobuf.ByteString
import io.github.thienvu18.kylok.proto.v1.*
import io.grpc.*
import io.grpc.okhttp.OkHttpChannelBuilder
import java.net.URI
import java.util.concurrent.TimeUnit

internal class AgentClient(private val channel: ManagedChannel) : AutoCloseable {
    companion object {
        /** Validate an origin and select its declared transport without TLS downgrade. */
        fun connect(origin: String): AgentClient {
            val uri = URI(origin)
            require(uri.scheme in setOf("http", "https") && !uri.host.isNullOrEmpty() && uri.userInfo == null &&
                uri.rawPath.isNullOrEmpty() && uri.rawQuery == null && uri.rawFragment == null)
            val port = if (uri.port != -1) uri.port else if (uri.scheme == "https") 443 else 80
            // Bound decoded snapshots to 1 MiB rather than accepting unbounded allocation from the endpoint.
            val builder = OkHttpChannelBuilder.forAddress(uri.host, port).maxInboundMessageSize(1024 * 1024)
            if (uri.scheme == "https") builder.useTransportSecurity() else builder.usePlaintext()
            return AgentClient(builder.build())
        }
    }
    /** Send the attempt-bound public key and inventory with a bounded RPC deadline. */
    fun enroll(token: String, attempt: String, publicKey: ByteArray, inventory: Inventory): EnrollResponse =
        AgentServiceGrpc.newBlockingStub(channel).withDeadlineAfter(20, TimeUnit.SECONDS).enroll(
            EnrollRequest.newBuilder().setToken(token).setAttemptId(attempt)
                .setPublicKey(ByteString.copyFrom(publicKey)).setInventory(inventory)
                .setProtocolVersion(BuildConfig.PROTOCOL_VERSION).build())

    /** Authenticate one persisted sync request using the device credential. */
    fun sync(request: SyncRequest, credential: String): SyncResponse {
        val headers = Metadata().apply {
            put(Metadata.Key.of("authorization", Metadata.ASCII_STRING_MARSHALLER), "Bearer $credential")
        }
        return AgentServiceGrpc.newBlockingStub(channel)
            .withInterceptors(io.grpc.stub.MetadataUtils.newAttachHeadersInterceptor(headers))
            .withDeadlineAfter(20, TimeUnit.SECONDS).sync(request)
    }
    /** Release the gRPC channel when the bounded worker exchange finishes. */
    override fun close() { channel.shutdownNow() }
}
