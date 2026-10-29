// SPDX-License-Identifier: LGPL-3.0-only
package android.os;

import java.util.*;

/** Test double for Bundle with Map backing. */
public final class Bundle extends BaseBundle implements Cloneable, Parcelable {
    public static final Bundle EMPTY = new Bundle();

    public static final Creator<Bundle> CREATOR = new Creator<Bundle>() {
        @Override public Bundle createFromParcel(Parcel in) { return new Bundle(); }
        @Override public Bundle[] newArray(int size) { return new Bundle[size]; }
    };

    public Bundle() {}
    public Bundle(Bundle b) { if (b != null) mMap.putAll(b.mMap); }
    public Bundle(PersistableBundle b) { if (b != null) mMap.putAll(b.mMap); }
    public Bundle(int capacity) {}
    public Bundle(ClassLoader loader) {}

    public Bundle getBundle(String key) {
        Object val = mMap.get(key);
        return val instanceof Bundle ? (Bundle) val : null;
    }
    public void putBundle(String key, Bundle value) { mMap.put(key, value); }

    public Parcelable[] getParcelableArray(String key) {
        Object val = mMap.get(key);
        return val instanceof Parcelable[] ? (Parcelable[]) val : null;
    }
    public void putParcelableArray(String key, Parcelable[] value) { mMap.put(key, value); }

    public void putAll(Bundle bundle) {
        if (bundle != null) mMap.putAll(bundle.mMap);
    }

    public Bundle deepCopy() {
        Bundle copy = new Bundle();
        copy.putAll(this);
        return copy;
    }

    @Override public int describeContents() { return 0; }
    @Override public void writeToParcel(Parcel dest, int flags) {}
    @Override public Object clone() { return deepCopy(); }
}
