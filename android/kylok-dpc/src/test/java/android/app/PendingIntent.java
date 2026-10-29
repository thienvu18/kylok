// SPDX-License-Identifier: LGPL-3.0-only
package android.app;

import android.content.Context;
import android.content.Intent;
import android.content.IntentSender;
import android.content.TestIntentSender;
import android.os.Bundle;
import android.os.Handler;
import android.os.Parcel;
import android.os.Parcelable;

/** Test double for PendingIntent. */
public final class PendingIntent implements Parcelable {
    public static final int FLAG_UPDATE_CURRENT = 134217728;
    public static final int FLAG_IMMUTABLE = 67108864;
    public static final int FLAG_MUTABLE = 33554432;
    public static final int FLAG_ONE_SHOT = 1073741824;
    public static final int FLAG_NO_CREATE = 536870912;
    public static final int FLAG_CANCEL_CURRENT = 268435456;
    public static final int FLAG_ALLOW_UNSAFE_IMPLICIT_INTENT = 16777216;

    public static final Creator<PendingIntent> CREATOR = new Creator<PendingIntent>() {
        @Override
        public PendingIntent createFromParcel(Parcel in) { return new PendingIntent(); }
        @Override
        public PendingIntent[] newArray(int size) { return new PendingIntent[size]; }
    };

    private final IntentSender sender = TestIntentSender.create();

    public PendingIntent() {}

    public IntentSender getIntentSender() {
        return sender;
    }

    public static PendingIntent getBroadcast(Context context, int requestCode, Intent intent, int flags) {
        return new PendingIntent();
    }

    public static PendingIntent getActivity(Context context, int requestCode, Intent intent, int flags) {
        return new PendingIntent();
    }

    public static PendingIntent getActivity(Context context, int requestCode, Intent intent, int flags, Bundle options) {
        return new PendingIntent();
    }

    public static PendingIntent getService(Context context, int requestCode, Intent intent, int flags) {
        return new PendingIntent();
    }

    public static PendingIntent getForegroundService(Context context, int requestCode, Intent intent, int flags) {
        return new PendingIntent();
    }

    @Override
    public int describeContents() { return 0; }

    @Override
    public void writeToParcel(Parcel dest, int flags) {}
}
