// SPDX-License-Identifier: LGPL-3.0-only
package android.net;

import java.io.Serializable;

/** Test double for Uri backed by java.net.URI parsing. */
public class Uri implements Comparable<Uri>, android.os.Parcelable, Serializable {
    private final String uriString;
    private final java.net.URI parsed;

    protected Uri(String uriString) {
        this.uriString = uriString;
        java.net.URI u = null;
        try {
            u = java.net.URI.create(uriString);
        } catch (Exception ignored) {}
        this.parsed = u;
    }

    public static Uri parse(String uriString) {
        return uriString != null ? new Uri(uriString) : null;
    }

    public String getScheme() {
        return parsed != null ? parsed.getScheme() : null;
    }

    public String getHost() {
        return parsed != null ? parsed.getHost() : null;
    }

    public String getPath() {
        return parsed != null ? parsed.getPath() : null;
    }

    public String getAuthority() {
        return parsed != null ? parsed.getAuthority() : null;
    }

    @Override
    public String toString() {
        return uriString;
    }

    @Override
    public boolean equals(Object o) {
        if (this == o) return true;
        if (o == null || getClass() != o.getClass()) return false;
        Uri uri = (Uri) o;
        return uriString != null ? uriString.equals(uri.uriString) : uri.uriString == null;
    }

    @Override
    public int hashCode() {
        return uriString != null ? uriString.hashCode() : 0;
    }

    @Override
    public int compareTo(Uri o) {
        return uriString.compareTo(o.uriString);
    }

    @Override
    public int describeContents() { return 0; }

    @Override
    public void writeToParcel(android.os.Parcel dest, int flags) {}
}
