// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import org.junit.Assert.*
import org.junit.Test

class NetworkEncodingTest {
    @Test fun literalAndroidStringsPreserveQuotesBackslashesAndUTF8Bytes() {
        for(value in listOf("Office", "Guest \"Wing\"", "Back\\Room", "Khách😀", "Line\nTwo")) {
            val quoted=NetworkEncoding.quoted(value)
            assertEquals(value,quoted.substring(1,quoted.length-1)) // NativeUtil removes enclosing quotes without JSON unescaping.
            assertEquals(value,NetworkEncoding.ssid(quoted))
            val hex=value.toByteArray(Charsets.UTF_8).joinToString("") { "%02x".format(it) }
            assertEquals(value,NetworkEncoding.ssid(hex))
        }
        val password="pass\\\"word"
        assertArrayEquals(password.toByteArray(),NetworkEncoding.quoted(password).drop(1).dropLast(1).toByteArray())
    }
    @Test fun numericIPAndDomainOriginsRetainCorrectSocketHostAndTLSAuthority() {
        for((origin,host,authority)in listOf(
            Triple("https://[::1]:8443","::1","[::1]:8443"),
            Triple("http://192.168.101.5:8443","192.168.101.5","192.168.101.5:8443"),
            Triple("https://kylok.test:8443","kylok.test","kylok.test:8443"))) {
            assertEquals(host,originAddressHost(java.net.URI(origin)))
            assertEquals(authority,originAuthority(host,8443))
        }
        assertEquals("[::1]:8443",originAuthority("[::1]",8443))
    }
    @Test fun unrelatedBinarySSIDsRemainDistinctFromManagedUTF8Profiles() {
        val binary=NetworkEncoding.ssid("ff80")
        assertTrue(binary.startsWith("\u0000hex:"))
        assertNotEquals(binary,NetworkEncoding.ssid("\"ff80\""))
    }
}
