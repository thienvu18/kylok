// SPDX-License-Identifier: LGPL-3.0-only
package android.content;

import android.net.Uri;
import android.os.Bundle;
import android.os.Parcelable;
import java.io.Serializable;
import java.util.*;

/** Test double for Intent backed by real Map/Bundle state. */
public class Intent implements Parcelable, Cloneable {
    public static final String ACTION_BOOT_COMPLETED = "android.intent.action.BOOT_COMPLETED";
    public static final String ACTION_MY_PACKAGE_REPLACED = "android.intent.action.MY_PACKAGE_REPLACED";
    public static final String ACTION_PACKAGE_ADDED = "android.intent.action.PACKAGE_ADDED";
    public static final String ACTION_PACKAGE_REPLACED = "android.intent.action.PACKAGE_REPLACED";
    public static final String ACTION_USER_PRESENT = "android.intent.action.USER_PRESENT";
    public static final String ACTION_MAIN = "android.intent.action.MAIN";
    public static final String ACTION_VIEW = "android.intent.action.VIEW";
    public static final String ACTION_POWER_CONNECTED = "android.intent.action.POWER_CONNECTED";
    public static final String CATEGORY_HOME = "android.intent.category.HOME";
    public static final String CATEGORY_DEFAULT = "android.intent.category.DEFAULT";
    public static final String CATEGORY_LAUNCHER = "android.intent.category.LAUNCHER";
    public static final String EXTRA_INTENT = "android.intent.extra.INTENT";
    public static final int FLAG_ACTIVITY_NEW_TASK = 0x10000000;
    public static final int FLAG_RECEIVER_REGISTERED_ONLY = 0x40000000;
    public static final int URI_INTENT_SCHEME = 1;

    private String mAction;
    private Uri mData;
    private String mType;
    private String mPackage;
    private ComponentName mComponent;
    private int mFlags;
    private final Set<String> mCategories = new HashSet<>();
    private Bundle mExtras = new Bundle();

    public Intent() {}

    public Intent(String action) {
        this.mAction = action;
    }

    public Intent(String action, Uri uri) {
        this.mAction = action;
        this.mData = uri;
    }

    public Intent(Context packageContext, Class<?> cls) {
        this.mComponent = new ComponentName(packageContext, cls);
    }

    public Intent(Intent o) {
        this.mAction = o.mAction;
        this.mData = o.mData;
        this.mType = o.mType;
        this.mPackage = o.mPackage;
        this.mComponent = o.mComponent;
        this.mFlags = o.mFlags;
        this.mCategories.addAll(o.mCategories);
        if (o.mExtras != null) {
            this.mExtras = new Bundle(o.mExtras);
        }
    }

    public String getAction() { return mAction; }
    public Intent setAction(String action) {
        this.mAction = action;
        return this;
    }

    public Uri getData() { return mData; }
    public Intent setData(Uri data) {
        this.mData = data;
        return this;
    }

    public String getType() { return mType; }
    public Intent setType(String type) {
        this.mType = type;
        return this;
    }

    public Intent setDataAndType(Uri data, String type) {
        this.mData = data;
        this.mType = type;
        return this;
    }

    public String getPackage() { return mPackage; }
    public Intent setPackage(String packageName) {
        this.mPackage = packageName;
        return this;
    }

    public ComponentName getComponent() { return mComponent; }
    public Intent setComponent(ComponentName component) {
        this.mComponent = component;
        return this;
    }

    public int getFlags() { return mFlags; }
    public Intent setFlags(int flags) {
        this.mFlags = flags;
        return this;
    }
    public Intent addFlags(int flags) {
        this.mFlags |= flags;
        return this;
    }

    public Intent addCategory(String category) {
        this.mCategories.add(category);
        return this;
    }
    public boolean hasCategory(String category) {
        return this.mCategories.contains(category);
    }
    public Set<String> getCategories() { return mCategories; }

    public Intent putExtra(String name, Bundle value) {
        mExtras.mMap.put(name, value);
        return this;
    }
    public Intent putExtra(String name, android.os.PersistableBundle value) {
        mExtras.mMap.put(name, value);
        return this;
    }
    public Intent putExtras(Bundle extras) {
        if (extras != null) {
            this.mExtras.putAll(extras);
        }
        return this;
    }

    public Intent putExtra(String name, String value) {
        mExtras.putString(name, value);
        return this;
    }
    public Intent putExtra(String name, boolean value) {
        mExtras.putBoolean(name, value);
        return this;
    }
    public Intent putExtra(String name, int value) {
        mExtras.putInt(name, value);
        return this;
    }
    public Intent putExtra(String name, long value) {
        mExtras.putLong(name, value);
        return this;
    }
    public Intent putExtra(String name, Parcelable value) {
        mExtras.mMap.put(name, value);
        return this;
    }
    public Intent putExtra(String name, Serializable value) {
        mExtras.mMap.put(name, value);
        return this;
    }
    public Intent putIntegerArrayListExtra(String name, ArrayList<Integer> value) {
        mExtras.mMap.put(name, value);
        return this;
    }
    public Intent putStringArrayListExtra(String name, ArrayList<String> value) {
        mExtras.mMap.put(name, value);
        return this;
    }

    public String getStringExtra(String name) { return mExtras.getString(name); }
    public boolean getBooleanExtra(String name, boolean defaultValue) { return mExtras.getBoolean(name, defaultValue); }
    public int getIntExtra(String name, int defaultValue) { return mExtras.getInt(name, defaultValue); }
    public long getLongExtra(String name, long defaultValue) { return mExtras.getLong(name, defaultValue); }
    public boolean hasExtra(String name) { return mExtras.containsKey(name); }
    public void removeExtra(String name) { mExtras.remove(name); }

    public <T> T getParcelableExtra(String name, Class<T> clazz) {
        Object val = mExtras.get(name);
        return clazz.isInstance(val) ? clazz.cast(val) : null;
    }

    @SuppressWarnings("unchecked")
    public ArrayList<Integer> getIntegerArrayListExtra(String name) {
        Object val = mExtras.get(name);
        return val instanceof ArrayList ? (ArrayList<Integer>) val : null;
    }

    @SuppressWarnings("unchecked")
    public ArrayList<String> getStringArrayListExtra(String name) {
        Object val = mExtras.get(name);
        return val instanceof ArrayList ? (ArrayList<String>) val : null;
    }

    public String toUri(int flags) {
        StringBuilder sb = new StringBuilder("intent:#Intent;");
        if (mAction != null) sb.append("action=").append(mAction).append(";");
        if (mPackage != null) sb.append("package=").append(mPackage).append(";");
        sb.append("end");
        return sb.toString();
    }

    public static Intent parseUri(String uriString, int flags) {
        Intent intent = new Intent();
        if (uriString != null && uriString.startsWith("intent:")) {
            String[] parts = uriString.split(";");
            for (String part : parts) {
                if (part.startsWith("action=")) intent.setAction(part.substring(7));
                else if (part.startsWith("package=")) intent.setPackage(part.substring(8));
            }
        }
        return intent;
    }

    public Intent cloneFilter() {
        Intent i = new Intent();
        i.mAction = mAction;
        i.mData = mData;
        i.mType = mType;
        i.mPackage = mPackage;
        i.mComponent = mComponent;
        i.mCategories.addAll(mCategories);
        return i;
    }

    @Override public int describeContents() { return 0; }
    @Override public void writeToParcel(android.os.Parcel dest, int flags) {}
}
