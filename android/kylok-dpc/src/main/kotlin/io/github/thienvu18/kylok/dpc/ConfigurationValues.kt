// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import io.github.thienvu18.kylok.proto.v1.*
import java.security.MessageDigest

/** Canonical bounded configuration encoding shared by policy hashing, journaling and Bundle readback. */
internal object ConfigurationValues {
    private val key = Regex("[A-Za-z][A-Za-z0-9_.-]{0,127}")
    private const val MAXIMUM_BYTES = 65536
    private const val MAXIMUM_ENTRIES = 256
    private const val MAXIMUM_DEPTH = 4
    /** Sort unique keys recursively while preserving array order and explicit scalar presence. */
    fun normalize(input: ConfigurationBundle): ConfigurationBundle {
        var nodes = 0
        /** Normalize a nested Bundle with a shared bound on all entries in this document. */
        fun bundle(value: ConfigurationBundle, depth: Int): ConfigurationBundle {
            require(depth <= MAXIMUM_DEPTH && value.entriesCount <= 64) { "Configuration nesting/key limit" }
            val seen = mutableSetOf<String>()
            val result = ConfigurationBundle.newBuilder()
            for (entry in value.entriesList.sortedBy { it.key }) {
                require(++nodes <= MAXIMUM_ENTRIES && entry.key.matches(key) && seen.add(entry.key) && entry.hasValue()) { "Invalid configuration key/value" }
                val normalized = entry.value.toBuilder()
                when (entry.value.valueCase) {
                    ConfigurationValue.ValueCase.BOOLEAN_VALUE, ConfigurationValue.ValueCase.INTEGER_VALUE -> Unit
                    ConfigurationValue.ValueCase.STRING_VALUE -> require(entry.value.stringValue.toByteArray(Charsets.UTF_8).size <= 4096)
                    ConfigurationValue.ValueCase.STRINGS_VALUE -> {
                        require(entry.value.stringsValue.valuesCount <= 64)
                        entry.value.stringsValue.valuesList.forEach { require(it.toByteArray(Charsets.UTF_8).size <= 4096) }
                    }
                    ConfigurationValue.ValueCase.BUNDLE_VALUE -> normalized.bundleValue = bundle(entry.value.bundleValue, depth + 1)
                    ConfigurationValue.ValueCase.BUNDLES_VALUE -> {
                        require(entry.value.bundlesValue.valuesCount <= 64)
                        normalized.bundlesValue = ConfigurationBundles.newBuilder().addAllValues(entry.value.bundlesValue.valuesList.map { bundle(it, depth + 1) }).build()
                    }
                    else -> error("Configuration value type required")
                }
                result.addEntries(entry.toBuilder().setValue(normalized))
            }
            return result.build()
        }
        return bundle(input, 1).also { require(it.serializedSize <= MAXIMUM_BYTES) { "Configuration payload limit" } }
    }
    /** Hash normalized protobuf bytes; reports never contain configuration values or journal encodings. */
    fun digest(input: ConfigurationBundle): String = "sha256:" + MessageDigest.getInstance("SHA-256")
        .digest(normalize(input).toByteArray()).joinToString("") { "%02x".format(it.toInt() and 0xff) }
    /** Canonicalize a journal encoding before exact platform comparison. */
    fun journal(input: ConfigurationBundle): String = encode(normalize(input))
    /** Decode protected journal content with validation before platform use. */
    fun read(value: String): ConfigurationBundle = normalize(ConfigurationBundle.parseFrom(decode(value)))
}
