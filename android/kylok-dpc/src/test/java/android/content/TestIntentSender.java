// SPDX-License-Identifier: LGPL-3.0-only
package android.content;

/** Factory for mockable IntentSender instances. */
public class TestIntentSender {
    public static IntentSender create() {
        return new IntentSender();
    }
}
