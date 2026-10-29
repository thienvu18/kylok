// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import io.github.thienvu18.kylok.dpc.storage.AgentRecord
import org.junit.After
import org.junit.Assert.*
import org.junit.Before
import org.junit.Test

/** Tests for AgentStorage encryption, key generation and transaction persistence. */
class AgentStorageTest {
    private lateinit var context: TestContext
    private lateinit var fakeDb: TestAgentDatabase

    @Before
    fun setUp() {
        TestAndroidKeyStoreProvider.install()
        TestAndroidKeyStoreProvider.reset()
        context = TestContext()
        fakeDb = TestAgentDatabase()

        val dbField = AgentStorage::class.java.getDeclaredField("database")
        dbField.isAccessible = true
        dbField.set(AgentStorage, fakeDb)
    }

    @After
    fun tearDown() {
        val dbField = AgentStorage::class.java.getDeclaredField("database")
        dbField.isAccessible = true
        dbField.set(AgentStorage, null)
        TestAndroidKeyStoreProvider.reset()
    }

    @Test
    fun testIdentityKeyGenerationAndCaching() {
        val pair1 = AgentStorage.identity()
        assertNotNull(pair1.public)
        assertNotNull(pair1.private)

        val pair2 = AgentStorage.identity()
        assertEquals(pair1.public, pair2.public)
        assertEquals(pair1.private, pair2.private)
    }

    @Test
    fun testReadEmptyStorageReturnsEmptyJson() {
        val json = AgentStorage.read(context)
        assertEquals(0, json.length())
    }

    @Test
    fun testUpdateAndReadRoundTrip() {
        AgentStorage.update(context) { state ->
            state.put("key1", "value1")
            state.put("count", 42)
        }

        assertNotNull(fakeDb.recordRef.get())
        val sealed = fakeDb.recordRef.get()!!.sealed
        assertTrue(sealed.size >= 28)

        val readBack = AgentStorage.read(context)
        assertEquals("value1", readBack.getString("key1"))
        assertEquals(42, readBack.getInt("count"))
    }

    @Test
    fun testAccessDelegation() {
        val access = AgentStorage.access(context)
        access.update { it.put("accessed", true) }
        val readBack = access.read()
        assertTrue(readBack.getBoolean("accessed"))
    }

    @Test
    fun testReadCorruptRecordFails() {
        val corrupt = AgentRecord().apply { sealed = byteArrayOf(1, 2, 3) } // less than 28 bytes
        fakeDb.recordRef.set(corrupt)
        assertThrows(IllegalArgumentException::class.java) {
            AgentStorage.read(context)
        }

        val badTag = AgentRecord().apply { sealed = ByteArray(32) } // invalid ciphertext / tag
        fakeDb.recordRef.set(badTag)
        assertThrows(Exception::class.java) {
            AgentStorage.read(context)
        }
    }
}
