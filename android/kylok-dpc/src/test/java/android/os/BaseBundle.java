// SPDX-License-Identifier: LGPL-3.0-only
package android.os;

import java.util.*;

/** Test double for BaseBundle with Map backing. */
public class BaseBundle {
    public final Map<String, Object> mMap = new HashMap<>();

    public BaseBundle() {}

    public void clear() { mMap.clear(); }
    public boolean containsKey(String key) { return mMap.containsKey(key); }
    public Object get(String key) { return mMap.get(key); }
    public boolean getBoolean(String key) { return getBoolean(key, false); }
    public boolean getBoolean(String key, boolean defaultValue) {
        Object val = mMap.get(key);
        return val instanceof Boolean ? (Boolean) val : defaultValue;
    }
    public void putBoolean(String key, boolean value) { mMap.put(key, value); }
    public int getInt(String key) { return getInt(key, 0); }
    public int getInt(String key, int defaultValue) {
        Object val = mMap.get(key);
        return val instanceof Number ? ((Number) val).intValue() : defaultValue;
    }
    public void putInt(String key, int value) { mMap.put(key, value); }
    public long getLong(String key) { return getLong(key, 0L); }
    public long getLong(String key, long defaultValue) {
        Object val = mMap.get(key);
        return val instanceof Number ? ((Number) val).longValue() : defaultValue;
    }
    public void putLong(String key, long value) { mMap.put(key, value); }
    public double getDouble(String key) { return getDouble(key, 0.0); }
    public double getDouble(String key, double defaultValue) {
        Object val = mMap.get(key);
        return val instanceof Number ? ((Number) val).doubleValue() : defaultValue;
    }
    public void putDouble(String key, double value) { mMap.put(key, value); }
    public String getString(String key) { return getString(key, null); }
    public String getString(String key, String defaultValue) {
        Object val = mMap.get(key);
        return val instanceof String ? (String) val : defaultValue;
    }
    public void putString(String key, String value) { mMap.put(key, value); }
    public String[] getStringArray(String key) {
        Object val = mMap.get(key);
        return val instanceof String[] ? (String[]) val : null;
    }
    public void putStringArray(String key, String[] value) { mMap.put(key, value); }
    public boolean[] getBooleanArray(String key) {
        Object val = mMap.get(key);
        return val instanceof boolean[] ? (boolean[]) val : null;
    }
    public void putBooleanArray(String key, boolean[] value) { mMap.put(key, value); }
    public int[] getIntArray(String key) {
        Object val = mMap.get(key);
        return val instanceof int[] ? (int[]) val : null;
    }
    public void putIntArray(String key, int[] value) { mMap.put(key, value); }
    public long[] getLongArray(String key) {
        Object val = mMap.get(key);
        return val instanceof long[] ? (long[]) val : null;
    }
    public void putLongArray(String key, long[] value) { mMap.put(key, value); }
    public double[] getDoubleArray(String key) {
        Object val = mMap.get(key);
        return val instanceof double[] ? (double[]) val : null;
    }
    public void putDoubleArray(String key, double[] value) { mMap.put(key, value); }
    public boolean isEmpty() { return mMap.isEmpty(); }
    public Set<String> keySet() { return mMap.keySet(); }
    public void remove(String key) { mMap.remove(key); }
    public int size() { return mMap.size(); }
    public void putAll(PersistableBundle bundle) {
        if (bundle != null) mMap.putAll(bundle.mMap);
    }
}
