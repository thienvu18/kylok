// SPDX-License-Identifier: LGPL-3.0-only
package android.os;

/** Test double for Looper returning active thread. */
public final class Looper {
    private static final Looper sMainLooper = new Looper(Thread.currentThread());

    private final Thread mThread;

    private Looper(Thread thread) {
        this.mThread = thread;
    }

    public static Looper getMainLooper() {
        return sMainLooper;
    }

    public static Looper myLooper() {
        return sMainLooper;
    }

    public Thread getThread() {
        return mThread;
    }

    public void quit() {}
    public void quitSafely() {}
}
