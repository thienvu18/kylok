// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

/** Immutable process-local host options. Reapply them in Application.onCreate after every process start.
 * These tune scheduling only; enrolled origins, credentials, policy checks and OS ownership remain SDK controlled.
 * Usage: `Kylok.configure(this, HostOptions(syncIntervalMinutes = 30))` in your Application.
 */
data class HostOptions(
    val syncIntervalMinutes: Long = 15,
    val retryBackoffSeconds: Long = 30,
    val wakeJitterSeconds: Int = 5
) {
    init {
        require(syncIntervalMinutes in 15..1440) { "Sync interval must be 15–1440 minutes" }
        require(retryBackoffSeconds in 30..3600) { "Retry backoff must be 30–3600 seconds" }
        require(wakeJitterSeconds in 0..300) { "Wake jitter must be 0–300 seconds" }
    }
}

/** One process's validated configuration; synchronize configure/freeze so a worker never sees partial changes. */
internal class HostConfiguration {
    private var options = HostOptions()
    private var frozen = false
    /** Accept equal repeated configuration; reject a conflicting override after any SDK work has started. */
    @Synchronized fun configure(value: HostOptions) {
        check(!frozen || value == options) { "Configure Kylok before its first work in this process" }
        options = value
    }
    /** Freeze one immutable configuration for component/worker starts without requiring an Activity. */
    @Synchronized fun start(): HostOptions { frozen = true; return options }
}
