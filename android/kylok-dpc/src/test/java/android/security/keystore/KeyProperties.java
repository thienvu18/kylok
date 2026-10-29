// SPDX-License-Identifier: LGPL-3.0-only
package android.security.keystore;

/** Test double for Android KeyProperties constants. */
public final class KeyProperties {
    public static final String KEY_ALGORITHM_AES = "AES";
    public static final String KEY_ALGORITHM_EC = "EC";
    public static final String BLOCK_MODE_GCM = "GCM";
    public static final String ENCRYPTION_PADDING_NONE = "NoPadding";
    public static final int PURPOSE_ENCRYPT = 1;
    public static final int PURPOSE_DECRYPT = 2;
    public static final int PURPOSE_AGREE_KEY = 64;

    private KeyProperties() {}
}
