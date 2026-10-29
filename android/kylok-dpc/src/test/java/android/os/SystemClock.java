// SPDX-License-Identifier: LGPL-3.0-only
package android.os;

/** Test double for SystemClock. */
public final class SystemClock {
    private SystemClock() {}

    public static long elapsedRealtime() {
        return System.currentTimeMillis();
    }

    public static long uptimeMillis() {
        return System.currentTimeMillis();
    }

    public static void sleep(long ms) {
        try {
            Thread.sleep(ms);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
    }
}
