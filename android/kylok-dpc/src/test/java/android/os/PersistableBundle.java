// SPDX-License-Identifier: LGPL-3.0-only
package android.os;

import java.util.*;

/** Test double for PersistableBundle with Map backing. */
public final class PersistableBundle extends BaseBundle implements Cloneable, Parcelable {
    public static final PersistableBundle EMPTY = new PersistableBundle();

    public static final Creator<PersistableBundle> CREATOR = new Creator<PersistableBundle>() {
        @Override public PersistableBundle createFromParcel(Parcel in) { return new PersistableBundle(); }
        @Override public PersistableBundle[] newArray(int size) { return new PersistableBundle[size]; }
    };

    public PersistableBundle() {}
    public PersistableBundle(PersistableBundle b) { if (b != null) mMap.putAll(b.mMap); }
    public PersistableBundle(int capacity) {}

    public PersistableBundle getPersistableBundle(String key) {
        Object val = mMap.get(key);
        return val instanceof PersistableBundle ? (PersistableBundle) val : null;
    }
    public void putPersistableBundle(String key, PersistableBundle value) { mMap.put(key, value); }

    public void putAll(PersistableBundle bundle) {
        if (bundle != null) mMap.putAll(bundle.mMap);
    }

    public PersistableBundle deepCopy() {
        PersistableBundle copy = new PersistableBundle();
        copy.putAll(this);
        return copy;
    }

    @Override public int describeContents() { return 0; }
    @Override public void writeToParcel(Parcel dest, int flags) {}
    @Override public Object clone() { return deepCopy(); }
}
