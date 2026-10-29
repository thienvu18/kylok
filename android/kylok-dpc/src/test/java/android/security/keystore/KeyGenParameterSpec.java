// SPDX-License-Identifier: LGPL-3.0-only
package android.security.keystore;

import java.security.spec.AlgorithmParameterSpec;

/** Test double for KeyGenParameterSpec. */
public final class KeyGenParameterSpec implements AlgorithmParameterSpec {
    private final String mKeystoreAlias;
    private final int mPurposes;

    public KeyGenParameterSpec(String alias, int purposes) {
        this.mKeystoreAlias = alias;
        this.mPurposes = purposes;
    }

    public String getKeystoreAlias() { return mKeystoreAlias; }
    public int getPurposes() { return mPurposes; }

    public static final class Builder {
        private final String mKeystoreAlias;
        private final int mPurposes;

        public Builder(String alias, int purposes) {
            this.mKeystoreAlias = alias;
            this.mPurposes = purposes;
        }

        public Builder setBlockModes(String... blockModes) { return this; }
        public Builder setEncryptionPaddings(String... paddings) { return this; }
        public Builder setKeySize(int keySize) { return this; }
        public Builder setAlgorithmParameterSpec(AlgorithmParameterSpec spec) { return this; }
        public KeyGenParameterSpec build() { return new KeyGenParameterSpec(mKeystoreAlias, mPurposes); }
    }
}
