// SPDX-License-Identifier: LGPL-3.0-only
package android.content.res;

import android.util.DisplayMetrics;

/** Test double for Resources returning non-null test strings. */
public class TestResources extends Resources {
    public TestResources() {
        super(new AssetManager(), new DisplayMetrics(), new Configuration());
    }

    @Override
    public String getString(int id) {
        return "TestString[" + id + "]";
    }

    @Override
    public String getString(int id, Object... formatArgs) {
        return "TestString[" + id + "]";
    }
}
