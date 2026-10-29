// SPDX-License-Identifier: LGPL-3.0-only
package android.content.pm;

import android.os.Parcel;
import android.os.Parcelable;

/** Test double for Signature. */
public class Signature implements Parcelable {
    private final byte[] mSignature;

    public Signature(byte[] signature) {
        this.mSignature = signature != null ? signature.clone() : new byte[0];
    }

    public Signature(String text) {
        this(text != null ? text.getBytes(java.nio.charset.StandardCharsets.UTF_8) : new byte[0]);
    }

    public byte[] toByteArray() {
        return mSignature.clone();
    }

    @Override public int describeContents() { return 0; }
    @Override public void writeToParcel(Parcel dest, int flags) {}
}
