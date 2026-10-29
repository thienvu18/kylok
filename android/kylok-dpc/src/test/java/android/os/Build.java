// SPDX-License-Identifier: LGPL-3.0-only
package android.os;

/** Test double for Build with configurable SDK_INT and hardware properties. */
public class Build {
    public static class VERSION {
        public static int SDK_INT = 0;
    }
    public static class VERSION_CODES {
        public static final int TIRAMISU = 33;
        public static final int UPSIDE_DOWN_CAKE = 34;
    }
    public static String[] SUPPORTED_ABIS = new String[] { "arm64-v8a", "armeabi-v7a" };
    public static String MODEL = "TestDevice";
    public static String MANUFACTURER = "TestMaker";
    public static String FINGERPRINT = "TestFingerprint";
}
