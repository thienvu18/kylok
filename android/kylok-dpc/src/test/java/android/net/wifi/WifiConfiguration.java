// SPDX-License-Identifier: LGPL-3.0-only
package android.net.wifi;

import java.util.BitSet;

/** Test double for WifiConfiguration providing real BitSets and fields. */
public class WifiConfiguration {
    public static final int SECURITY_TYPE_OPEN = 0;
    public static final int SECURITY_TYPE_WEP = 1;
    public static final int SECURITY_TYPE_PSK = 2;
    public static final int SECURITY_TYPE_EAP = 3;
    public static final int SECURITY_TYPE_SAE = 4;

    public static class KeyMgmt {
        public static final int NONE = 0;
        public static final int WPA_PSK = 1;
        public static final int WPA_EAP = 2;
        public static final int IEEE8021X = 3;
        public static final int OWE = 6;
        public static final int SAE = 8;
    }

    public static class Protocol {
        public static final int WPA = 0;
        public static final int RSN = 1;
    }

    public static class PairwiseCipher {
        public static final int NONE = 0;
        public static final int TKIP = 1;
        public static final int CCMP = 2;
        public static final int GCMP_256 = 3;
    }

    public static class GroupCipher {
        public static final int WEP40 = 0;
        public static final int WEP104 = 1;
        public static final int TKIP = 2;
        public static final int CCMP = 3;
        public static final int GCMP_256 = 5;
    }

    public int networkId = -1;
    public String SSID;
    public boolean hiddenSSID;
    public String preSharedKey;
    public String[] wepKeys = new String[4];
    public BitSet allowedKeyManagement = new BitSet();
    public BitSet allowedProtocols;
    public BitSet allowedPairwiseCiphers;
    public BitSet allowedGroupCiphers;

    public WifiConfiguration() {}

    public void setSecurityParams(int securityType) {
        allowedKeyManagement.clear();
        switch (securityType) {
            case SECURITY_TYPE_OPEN:
                allowedKeyManagement.set(KeyMgmt.NONE);
                break;
            case SECURITY_TYPE_PSK:
                allowedKeyManagement.set(KeyMgmt.WPA_PSK);
                break;
            case SECURITY_TYPE_SAE:
                allowedKeyManagement.set(KeyMgmt.SAE);
                break;
            default:
                break;
        }
    }
}
