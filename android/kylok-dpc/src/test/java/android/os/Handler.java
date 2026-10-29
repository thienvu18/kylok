// SPDX-License-Identifier: LGPL-3.0-only
package android.os;

/** Test double for Handler synchronously executing posted Runnables. */
public class Handler {
    private final Looper mLooper;

    public Handler() {
        this(Looper.myLooper());
    }

    public Handler(Looper looper) {
        this.mLooper = looper;
    }

    public Looper getLooper() {
        return mLooper;
    }

    public boolean post(Runnable r) {
        if (r != null) {
            r.run();
        }
        return true;
    }

    public Runnable lastDelayedRunnable;
    public long lastDelayMillis;

    public boolean postDelayed(Runnable r, long delayMillis) {
        this.lastDelayedRunnable = r;
        this.lastDelayMillis = delayMillis;
        return true;
    }

    public void removeCallbacksAndMessages(Object token) {}
    public void removeCallbacks(Runnable r) {}
}
