// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import io.github.thienvu18.kylok.proto.v1.Command
import io.github.thienvu18.kylok.proto.v1.CommandObservation
import io.github.thienvu18.kylok.proto.v1.CommandReport

/** Keep privileged one-shot effects separate from their durable delivery and observation. */
internal interface CommandPlatform {
    /** Read the absolute deadline clock. */
    fun now(): Long
    /** Identify this process so a crash between checkpoint and effect stays uncertain rather than replaying. */
    fun processID(): String
    /** Recheck Device Owner immediately before a privileged effect. */
    fun isOwner(): Boolean
    /** Read Android's boot counter, or -1 if no trustworthy readback is available. */
    fun bootCount(): Long
    /** Read whether Android has a PIN, pattern or password configured. */
    fun isSecure(): Boolean
    /** Read whether Android currently requires unlocking the device. */
    fun isLocked(): Boolean
    /** Request one Device Owner reboot; a return value alone never establishes success. */
    fun reboot()
    /** Request immediate locking without changing the user's credential. */
    fun lock()
}

/** Execute each dispatch at most once after an atomic pre-effect checkpoint; ambiguity is a terminal result. */
internal class CommandEngine(private val storage: StateAccess, private val platform: CommandPlatform) {
    companion object {
        // Keyguard readback can lag lockNow; allow one short observation interval without repeating the effect.
        private const val LOCK_OBSERVATION_MILLIS = 10_000L
    }

    /** Persist one terminal report with the original dispatch token, keeping it until a server receipt. */
    private fun finish(command: Command, state: String, code: String = "", observation: CommandObservation? = null) {
        val report = CommandReport.newBuilder().setCommandId(command.id).setDispatchToken(command.dispatchToken)
            .setState(state).setErrorCode(code)
        if (observation != null) report.observation = observation
        storage.update { current ->
            current.objectValue("command_reports").put(command.id, encode(report.build()))
            current.objectValue("commands").getJSONObject(command.id).put("stage", "terminal")
        }
    }

    /** Observe a checkpointed effect without ever invoking it again, including after boot or process recreation. */
    private fun observe(command: Command): Boolean {
        val record = storage.read().objectValue("commands").getJSONObject(command.id)
        val before = record.getLong("boot_before")
        val after = platform.bootCount()
        val observation = CommandObservation.newBuilder().setBootBefore(before).setBootAfter(after)
            .setSecure(platform.isSecure()).setLocked(platform.isLocked()).build()
        if (command.kind == "REBOOT" && before >= 0 && after > before || command.kind == "LOCK" && observation.secure && observation.locked) {
            finish(command, "SUCCEEDED", observation = observation); return false
        }
        val sameProcess = record.getString("process") == platform.processID()
        val waiting = platform.now() < command.deadlineUnixMillis && sameProcess &&
            (command.kind == "REBOOT" || platform.now() - record.getLong("armed_at") in 0 until LOCK_OBSERVATION_MILLIS)
        if (waiting) return true
        finish(command, "UNCERTAIN", "EFFECT_NOT_OBSERVED", observation); return false
    }

    /** Reconcile delivered commands; report observations before dispatching another effect in a later sync. */
    fun run(): Boolean {
        var pending = false
        val commands = storage.read().objectValue("commands")
        for (id in commands.keys().asSequence().sorted().sortedBy { if (commands.getJSONObject(it).optString("stage") == "armed") 0 else 1 }) {
            val record = commands.getJSONObject(id)
            val command = Command.parseFrom(decode(record.getString("command")))
            if (record.optString("stage") == "terminal") continue
            if (record.optString("stage") == "armed") { pending = observe(command) || pending; continue }
            if (pending) continue
            if (!platform.isOwner()) { finish(command, "FAILED", "DEVICE_OWNER_REQUIRED"); continue }
            if (platform.now() >= command.deadlineUnixMillis) { finish(command, "FAILED", "DEADLINE_EXCEEDED"); continue }
            if (command.kind !in setOf("REBOOT", "LOCK")) { finish(command, "FAILED", "COMMAND_UNSUPPORTED"); continue }
            val boot = platform.bootCount()
            if (command.kind == "REBOOT" && boot < 0) { finish(command, "FAILED", "BOOT_COUNT_UNAVAILABLE"); continue }
            if (command.kind == "LOCK" && !platform.isSecure()) { finish(command, "FAILED", "SECURE_LOCK_REQUIRED"); continue }
            // A crash at any point after this commit must not authorize another reboot/lock.
            storage.update { state -> state.objectValue("commands").getJSONObject(id).put("stage", "armed")
                .put("boot_before", boot).put("process", platform.processID()).put("armed_at", platform.now()) }
            try {
                if (platform.now() >= command.deadlineUnixMillis) { finish(command, "FAILED", "DEADLINE_EXCEEDED"); continue }
                if (command.kind == "REBOOT") platform.reboot() else platform.lock()
            } catch (_: SecurityException) { finish(command, "FAILED", "PLATFORM_PERMISSION_DENIED"); continue }
            catch (_: IllegalStateException) { finish(command, "FAILED", "PLATFORM_REJECTED"); continue }
            catch (_: RuntimeException) { finish(command, "UNCERTAIN", "PLATFORM_ERROR"); continue }
            pending = observe(command) || pending
        }
        return pending
    }
}
