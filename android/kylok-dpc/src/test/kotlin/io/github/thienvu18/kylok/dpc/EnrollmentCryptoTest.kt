// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import com.google.protobuf.ByteString
import io.github.thienvu18.kylok.proto.v1.*
import org.junit.Assert.*
import org.junit.Test
import java.security.KeyPair
import java.security.KeyPairGenerator
import java.security.spec.ECGenParameterSpec
import javax.crypto.*
import javax.crypto.spec.*

class EnrollmentCryptoTest {
    private fun key(): KeyPair = KeyPairGenerator.getInstance("EC").apply {
        initialize(ECGenParameterSpec("secp256r1"))
    }.generateKeyPair()
    private fun envelope(recipient: KeyPair): EnrollResponse {
        val ephemeral = key()
        val agreement = KeyAgreement.getInstance("ECDH")
        agreement.init(ephemeral.private); agreement.doPhase(recipient.public, true)
        val hmac = Mac.getInstance("HmacSHA256")
        hmac.init(SecretKeySpec(ByteArray(32), "HmacSHA256"))
        val prk = hmac.doFinal(agreement.generateSecret())
        hmac.init(SecretKeySpec(prk, "HmacSHA256"))
        val derived = hmac.doFinal("kylok/enrollment/v1".toByteArray() + byteArrayOf(1))
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, SecretKeySpec(derived, "AES"))
        cipher.updateAAD("kylok/v1\n${"d".repeat(43)}\nsite-a\nattempt-0123456789".toByteArray())
        return EnrollResponse.newBuilder().setProtocolVersion(1)
            .setDevice(Device.newBuilder().setId("d".repeat(43)).setScopeId("site-a"))
            .setCredential(CredentialEnvelope.newBuilder()
                .setEphemeralPublicKey(ByteString.copyFrom(ephemeral.public.encoded))
                .setNonce(ByteString.copyFrom(cipher.iv))
                .setCiphertext(ByteString.copyFrom(cipher.doFinal("c".repeat(43).toByteArray())))).build()
    }
    private fun fails(block: () -> Unit) {
        try { block(); fail("Expected credential authentication to fail") }
        catch (expected: java.security.GeneralSecurityException) { }
        catch (expected: IllegalArgumentException) { }
    }
    @Test fun decryptsBoundCredential() {
        val key = key()
        assertEquals("c".repeat(43), EnrollmentCrypto.open(key.private, envelope(key), "attempt-0123456789"))
    }
    @Test fun copiedResponseCannotBeOpenedByAnotherDevice() {
        fails { EnrollmentCrypto.open(key().private, envelope(key()), "attempt-0123456789") }
    }
    @Test fun scopeTamperingIsRejected() {
        val key = key(); val r = envelope(key)
        fails { EnrollmentCrypto.open(key.private, r.toBuilder().setDevice(r.device.toBuilder().setScopeId("site-b")).build(), "attempt-0123456789") }
    }
    @Test fun attemptTamperingIsRejected() {
        val key = key()
        fails { EnrollmentCrypto.open(key.private, envelope(key), "different-attempt") }
    }
    @Test fun modifiedCiphertextIsRejected() {
        val key = key(); val r = envelope(key); val bytes = r.credential.ciphertext.toByteArray()
        bytes[0] = (bytes[0].toInt() xor 1).toByte()
        fails { EnrollmentCrypto.open(key.private, r.toBuilder().setCredential(r.credential.toBuilder()
            .setCiphertext(ByteString.copyFrom(bytes))).build(), "attempt-0123456789") }
    }
    @Test fun unknownProtocolIsRejected() {
        val key = key()
        fails { EnrollmentCrypto.open(key.private, envelope(key).toBuilder().setProtocolVersion(2).build(), "attempt-0123456789") }
    }
}
