// SPDX-License-Identifier: LGPL-3.0-only
package android.content.pm;

import android.os.Parcel;
import android.os.Parcelable;

/** Test double for SigningInfo. */
public final class SigningInfo implements Parcelable {
    public Signature[] apkContentsSigners;
    public Signature[] signingCertificateHistory;
    public boolean multipleSigners = false;

    public SigningInfo() {}

    public SigningInfo(Signature[] signers) {
        this.apkContentsSigners = signers;
    }

    public boolean hasMultipleSigners() {
        return multipleSigners;
    }

    public Signature[] getApkContentsSigners() {
        return apkContentsSigners;
    }

    public Signature[] getSigningCertificateHistory() {
        return signingCertificateHistory;
    }

    @Override public int describeContents() { return 0; }
    @Override public void writeToParcel(Parcel dest, int flags) {}
}
