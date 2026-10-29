// SPDX-License-Identifier: LGPL-3.0-only
package android.net;

import android.os.Parcel;
import android.os.Parcelable;

/** Test double for NetworkCapabilities. */
public class NetworkCapabilities implements Parcelable {
    public static final int TRANSPORT_WIFI = 1;

    public boolean wifiTransport = true;
    public TransportInfo transportInfo;

    public boolean hasTransport(int transportType) {
        return transportType == TRANSPORT_WIFI && wifiTransport;
    }

    public TransportInfo getTransportInfo() {
        return transportInfo;
    }

    @Override public int describeContents() { return 0; }
    @Override public void writeToParcel(Parcel dest, int flags) {}
}
