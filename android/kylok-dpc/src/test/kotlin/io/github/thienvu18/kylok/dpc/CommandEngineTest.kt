// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import io.github.thienvu18.kylok.proto.v1.Command
import io.github.thienvu18.kylok.proto.v1.CommandReport
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class CommandEngineTest {
    private val command = Command.newBuilder().setId("c".repeat(43)).setKind("REBOOT")
        .setDispatchToken("a".repeat(48)).setDeadlineUnixMillis(100_000).build()
    private fun state(kind: String = "REBOOT") = MemoryState(JSONObject().put("commands", JSONObject()
        .put(command.id, JSONObject().put("command", encode(command.toBuilder().setKind(kind).build())))))
    private fun report(storage: StateAccess) = CommandReport.parseFrom(decode(storage.read().objectValue("command_reports").getString(command.id)))
    private class Death : Error()
    private class Platform : CommandPlatform {
        var time = 1L; var process = "one"; var owner = true; var boot = 7L; var secure = true; var locked = false
        var reboots = 0; var locks = 0; var die = false; var reject = false; var lockObservable = true
        override fun now() = time
        override fun processID() = process
        override fun isOwner() = owner
        override fun bootCount() = boot
        override fun isSecure() = secure
        override fun isLocked() = locked
        override fun reboot() { if (reject) throw IllegalStateException("ongoing call"); reboots++; if (die) throw Death() }
        override fun lock() { locks++; locked = lockObservable }
    }
    @Test fun rebootNeedsBootReadbackAndCannotReplayOnResponseLoss() {
        val storage = state(); val platform = Platform()
        assertTrue(CommandEngine(storage, platform).run())
        assertEquals(1, platform.reboots)
        assertTrue(CommandEngine(MemoryState(storage.read()), platform).run())
        assertEquals(1, platform.reboots)
        assertEquals(0, storage.read().objectValue("command_reports").length())
        platform.boot++; platform.process = "after boot"
        assertFalse(CommandEngine(storage, platform).run())
        assertEquals("SUCCEEDED", report(storage).state)
        assertEquals(7L, report(storage).observation.bootBefore)
        assertEquals(8L, report(storage).observation.bootAfter)
        val result = report(storage)
        CommandEngine(storage, platform).run()
        assertEquals(result, report(storage)); assertEquals(1, platform.reboots)
    }
    @Test fun crashAfterArmingBeforeEffectStaysUncertain() {
        val storage = state(); val platform = Platform()
        val failAfterCommit = object : StateAccess {
            override fun read() = storage.read()
            override fun update(change: (JSONObject) -> Unit) { storage.update(change); throw Death() }
        }
        try { CommandEngine(failAfterCommit, platform).run(); fail("death not injected") } catch (_: Death) { }
        assertEquals(0, platform.reboots)
        platform.process = "restart"
        CommandEngine(storage, platform).run()
        assertEquals("UNCERTAIN", report(storage).state)
        assertEquals(0, platform.reboots)
    }
    @Test fun failedPreEffectWriteDoesNotInvokeAndroid() {
        val storage = state(); val platform = Platform()
        val full = object : StateAccess {
            override fun read() = storage.read()
            override fun update(change: (JSONObject) -> Unit) { throw java.io.IOException("disk full") }
        }
        try { CommandEngine(full, platform).run(); fail("write not failed") } catch (_: java.io.IOException) { }
        assertEquals(0, platform.reboots)
        assertFalse(storage.read().objectValue("commands").getJSONObject(command.id).has("stage"))
    }
    @Test fun deathDuringRebootCannotCauseAnotherReboot() {
        val storage = state(); val platform = Platform().apply { die = true }
        try { CommandEngine(storage, platform).run(); fail("death not injected") } catch (_: Death) { }
        platform.process = "restart"; platform.die = false
        CommandEngine(storage, platform).run()
        assertEquals("UNCERTAIN", report(storage).state)
        assertEquals(1, platform.reboots)
    }
    @Test fun lockNeedsSecureLockedObservation() {
        val storage = state("LOCK"); val platform = Platform()
        assertFalse(CommandEngine(storage, platform).run())
        assertEquals("SUCCEEDED", report(storage).state); assertEquals(1, platform.locks)
        CommandEngine(storage, platform).run(); assertEquals(1, platform.locks)
        val unsecure = state("LOCK"); platform.secure = false
        CommandEngine(unsecure, platform).run()
        assertEquals("SECURE_LOCK_REQUIRED", report(unsecure).errorCode)
        assertEquals(1, platform.locks)
    }
    @Test fun delayedKeyguardObservationDoesNotRepeatLock() {
        val storage = state("LOCK"); val platform = Platform().apply { lockObservable = false }
        assertTrue(CommandEngine(storage, platform).run()); assertEquals(1, platform.locks)
        platform.locked = true; platform.time++
        assertFalse(CommandEngine(storage, platform).run()); assertEquals(1, platform.locks)
        assertEquals("SUCCEEDED", report(storage).state)
    }
    @Test fun lockObservationTimeoutAndRebootDeadlineAreUncertain() {
        for (kind in listOf("LOCK", "REBOOT")) {
            val storage = state(kind); val platform = Platform().apply { lockObservable = false }
            assertTrue(CommandEngine(storage, platform).run())
            platform.time = 100_000
            assertFalse(CommandEngine(storage, platform).run())
            assertEquals("UNCERTAIN", report(storage).state)
            assertEquals(1, platform.reboots + platform.locks)
        }
    }
    @Test fun rejectedExpiredUnobservableAndUnownedCommandsNeverSucceed() {
        for (code in listOf("PLATFORM_REJECTED", "DEADLINE_EXCEEDED", "BOOT_COUNT_UNAVAILABLE", "DEVICE_OWNER_REQUIRED")) {
            val storage = state(); val platform = Platform()
            when (code) {
                "PLATFORM_REJECTED" -> platform.reject = true
                "DEADLINE_EXCEEDED" -> platform.time = 100_000
                "BOOT_COUNT_UNAVAILABLE" -> platform.boot = -1
                "DEVICE_OWNER_REQUIRED" -> platform.owner = false
            }
            CommandEngine(storage, platform).run()
            assertEquals("FAILED", report(storage).state); assertEquals(code, report(storage).errorCode)
            assertEquals(0, platform.reboots)
        }

        // SecurityException -> PLATFORM_PERMISSION_DENIED
        val secStorage = state()
        val secPlatform = object : CommandPlatform by Platform() {
            override fun reboot() { throw SecurityException("denied") }
        }
        CommandEngine(secStorage, secPlatform).run()
        assertEquals("FAILED", report(secStorage).state)
        assertEquals("PLATFORM_PERMISSION_DENIED", report(secStorage).errorCode)

        // RuntimeException -> UNCERTAIN, PLATFORM_ERROR
        val errStorage = state()
        val errPlatform = object : CommandPlatform by Platform() {
            override fun reboot() { throw RuntimeException("generic failure") }
        }
        CommandEngine(errStorage, errPlatform).run()
        assertEquals("UNCERTAIN", report(errStorage).state)
        assertEquals("PLATFORM_ERROR", report(errStorage).errorCode)

        // COMMAND_UNSUPPORTED
        val unsupStorage = state("UNSUPPORTED_KIND")
        CommandEngine(unsupStorage, Platform()).run()
        assertEquals("FAILED", report(unsupStorage).state)
        assertEquals("COMMAND_UNSUPPORTED", report(unsupStorage).errorCode)
    }
}
