// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import io.github.thienvu18.kylok.proto.v1.EnrollResponse
import java.nio.charset.StandardCharsets.UTF_8
import java.security.KeyFactory
import java.security.PrivateKey
import java.security.spec.X509EncodedKeySpec
import javax.crypto.Cipher
import javax.crypto.KeyAgreement
import javax.crypto.Mac
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec

internal object EnrollmentCrypto {
    /** Validate and decrypt a credential bound to device, scope and enrollment attempt. */
    fun open(privateKey: PrivateKey, response: EnrollResponse, attempt: String): String {
        require(response.protocolVersion == BuildConfig.PROTOCOL_VERSION && response.hasDevice() && response.hasCredential())
        require(response.device.id.matches(Regex("[A-Za-z0-9_-]{43}")))
        require(response.device.scopeId.matches(Regex("[A-Za-z0-9][A-Za-z0-9_.-]{0,127}")))
        val envelope = response.credential
        // Protocol 1 encrypts a 43-byte base64url credential plus a 16-byte GCM tag with a 12-byte nonce.
        require(envelope.nonce.size() == 12 && envelope.ciphertext.size() == 59)
        val ephemeral = KeyFactory.getInstance("EC").generatePublic(
            X509EncodedKeySpec(envelope.ephemeralPublicKey.toByteArray()))
        val agreement = KeyAgreement.getInstance("ECDH")
        agreement.init(privateKey)
        agreement.doPhase(ephemeral, true)
        val key = hkdf(agreement.generateSecret())
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, SecretKeySpec(key, "AES"),
            GCMParameterSpec(128, envelope.nonce.toByteArray()))
        cipher.updateAAD("kylok/v1\n${response.device.id}\n${response.device.scopeId}\n$attempt".toByteArray(UTF_8))
        val credential = String(cipher.doFinal(envelope.ciphertext.toByteArray()), UTF_8)
        require(credential.matches(Regex("[A-Za-z0-9_-]{43}")))
        return credential
    }

    /** Derive the shared AES key using the protocol HKDF-SHA256 salt, label and first expansion block. */
    private fun hkdf(shared: ByteArray): ByteArray {
        val hmac = Mac.getInstance("HmacSHA256")
        // RFC 5869: absent salt is HashLen zero bytes; one expansion block yields the 32-byte AES key.
        hmac.init(SecretKeySpec(ByteArray(32), "HmacSHA256"))
        val prk = hmac.doFinal(shared)
        hmac.init(SecretKeySpec(prk, "HmacSHA256"))
        return hmac.doFinal("kylok/enrollment/v1".toByteArray(UTF_8) + byteArrayOf(1))
    }
}
