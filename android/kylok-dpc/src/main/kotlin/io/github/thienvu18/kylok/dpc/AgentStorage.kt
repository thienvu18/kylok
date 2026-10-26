// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import androidx.room.Room
import io.github.thienvu18.kylok.dpc.storage.AgentDatabase
import io.github.thienvu18.kylok.dpc.storage.AgentRecord
import org.json.JSONObject
import java.security.KeyPair
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.spec.ECGenParameterSpec
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** All state changes are serialized in-process; Room transactions commit before network effects. */
internal object AgentStorage {
    private const val SEAL_ALIAS = "kylok.state.v1"
    private const val IDENTITY_ALIAS = "kylok.enrollment.v1"
    private var database: AgentDatabase? = null
    /** Reuse one Room database for serialized encrypted state transactions. */
    private fun db(context: Context): AgentDatabase = database ?: Room.databaseBuilder(
        context.applicationContext, AgentDatabase::class.java, "kylok-agent.db"
    ).build().also { database = it }
    /** Open Android Keystore without exporting the protected keys. */
    private fun keys() = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }

    /** Load or create the AES key used to authenticate and encrypt local state. */
    @Synchronized private fun sealKey(): SecretKey {
        val store = keys()
        if (store.containsAlias(SEAL_ALIAS)) return store.getKey(SEAL_ALIAS, null) as SecretKey
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
        generator.init(KeyGenParameterSpec.Builder(SEAL_ALIAS,
            KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
            .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
            .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).setKeySize(256).build())
        return generator.generateKey()
    }
    /** Reuse the non-exportable P-256 enrollment key across response retries. */
    @Synchronized fun identity(): KeyPair {
        val store = keys()
        if (store.containsAlias(IDENTITY_ALIAS)) return KeyPair(store.getCertificate(IDENTITY_ALIAS).publicKey,
            store.getKey(IDENTITY_ALIAS, null) as java.security.PrivateKey)
        val generator = KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, "AndroidKeyStore")
        generator.initialize(KeyGenParameterSpec.Builder(IDENTITY_ALIAS, KeyProperties.PURPOSE_AGREE_KEY)
            .setAlgorithmParameterSpec(ECGenParameterSpec("secp256r1")).build())
        return generator.generateKeyPair()
    }
    /** Authenticate and decrypt the singleton state record; fail on corrupt or unavailable state. */
    @Synchronized fun read(context: Context): JSONObject {
        val record = db(context).agent().load() ?: return JSONObject()
        val sealed = record.sealed
        // AES-GCM stores a 12-byte nonce before ciphertext with a 16-byte authentication tag.
        require(sealed.size >= 28)
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, sealKey(), GCMParameterSpec(128, sealed.copyOfRange(0, 12)))
        // Bind ciphertext to this state format so it cannot be reused as another envelope type.
        cipher.updateAAD("kylok-agent-state-v1".toByteArray(Charsets.UTF_8))
        return JSONObject(String(cipher.doFinal(sealed.copyOfRange(12, sealed.size)), Charsets.UTF_8))
    }
    /** Bind the durable Android store to the reconciliation engines. */
    fun access(context: Context): StateAccess = object : StateAccess {
        /** Authenticate and decrypt the singleton state record; fail on corrupt or unavailable state. */
        override fun read() = AgentStorage.read(context)
        /** Encrypt and commit a complete state change inside a Room transaction. */
        override fun update(change: (JSONObject) -> Unit) = AgentStorage.update(context, change)
    }
    /** Encrypt and commit a complete state change inside a Room transaction. */
    @Synchronized fun update(context: Context, change: (JSONObject) -> Unit) {
        val db = db(context)
        db.runInTransaction {
            val state = read(context)
            change(state)
            val cipher = Cipher.getInstance("AES/GCM/NoPadding")
            cipher.init(Cipher.ENCRYPT_MODE, sealKey())
            cipher.updateAAD("kylok-agent-state-v1".toByteArray(Charsets.UTF_8))
            val record = AgentRecord().apply {
                sealed = cipher.iv + cipher.doFinal(state.toString().toByteArray(Charsets.UTF_8))
            }
            db.agent().save(record)
        }
    }
}
