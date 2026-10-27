// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

/** Android saved-network strings use literal enclosing quotes or hexadecimal bytes, without JSON unescaping. */
internal object NetworkEncoding {
    /** Preserve literal quotes/backslashes inside SSIDs and passphrases using Android's enclosing-quote format. */
    fun quoted(value: String): String = "\"$value\""
    /** Decode Android's saved SSID while keeping unrelated non-UTF-8 SSIDs distinguishable and untouched. */
    fun ssid(value: String): String {
        if (value.length >= 2 && value.first() == '"' && value.last() == '"') return value.substring(1,value.length-1)
        if (value.length !in 2..64 || value.length % 2 != 0 || !value.matches(Regex("[0-9a-fA-F]+"))) throw NetworkRejected("SSID_UNOBSERVABLE")
        val bytes=value.chunked(2).map { it.toInt(16).toByte() }.toByteArray()
        return try {
            Charsets.UTF_8.newDecoder().onMalformedInput(java.nio.charset.CodingErrorAction.REPORT)
                .onUnmappableCharacter(java.nio.charset.CodingErrorAction.REPORT).decode(java.nio.ByteBuffer.wrap(bytes)).toString()
        } catch (_: java.nio.charset.CharacterCodingException) {
            // Managed UTF-8 profiles reject NUL, so this opaque identity cannot collide with their literal SSIDs.
            "\u0000hex:${value.lowercase()}"
        }
    }
}
