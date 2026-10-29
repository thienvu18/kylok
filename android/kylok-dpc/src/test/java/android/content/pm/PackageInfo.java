// SPDX-License-Identifier: LGPL-3.0-only
package android.content.pm;

import android.os.Parcel;
import android.os.Parcelable;

/** Test double for PackageInfo. */
public class PackageInfo implements Parcelable {
    public String packageName;
    public int versionCode;
    public long longVersionCode;
    public ApplicationInfo applicationInfo;
    public SigningInfo signingInfo;
    public String[] splitNames;
    public FeatureInfo[] reqFeatures;
    public String[] requestedPermissions;

    public PackageInfo() {}

    public long getLongVersionCode() {
        return longVersionCode != 0 ? longVersionCode : versionCode;
    }

    public void setLongVersionCode(long longVersionCode) {
        this.longVersionCode = longVersionCode;
    }

    @Override public int describeContents() { return 0; }
    @Override public void writeToParcel(Parcel dest, int flags) {}
}
