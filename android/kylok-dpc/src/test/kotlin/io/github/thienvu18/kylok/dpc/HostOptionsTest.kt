// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import org.junit.Assert.*
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors

class HostOptionsTest {
    @Test fun defaultsAndProcessRecreation() {
        val process = HostConfiguration()
        val configured = HostOptions(syncIntervalMinutes = 30, wakeJitterSeconds = 0)
        process.configure(configured)
        assertEquals(configured, process.start())
        process.configure(configured)
        try { process.configure(HostOptions()); fail("changed frozen options") } catch (_: IllegalStateException) { }
        assertEquals(HostOptions(), HostConfiguration().start())
        for (invalid in listOf<() -> HostOptions>({ HostOptions(syncIntervalMinutes = 14) }, { HostOptions(retryBackoffSeconds = 1) }, { HostOptions(wakeJitterSeconds = 301) })) {
            try { invalid(); fail("invalid scheduling configuration") } catch (_: IllegalArgumentException) { }
        }
    }
    @Test fun componentAndConfigurationRaceFreezesOneCompleteValue() {
        repeat(50) {
            val process = HostConfiguration()
            val selected = HostOptions(syncIntervalMinutes = 30, retryBackoffSeconds = 60, wakeJitterSeconds = 0)
            val executor = Executors.newFixedThreadPool(2)
            val start = CountDownLatch(1)
            try {
                val config = executor.submit { start.await(); runCatching { process.configure(selected) } }
                val worker = executor.submit<HostOptions> { start.await(); process.start() }
                start.countDown(); config.get()
                val observed = worker.get()
                assertTrue(observed == HostOptions() || observed == selected)
                assertEquals(observed, process.start())
            } finally { executor.shutdownNow() }
        }
    }
}
