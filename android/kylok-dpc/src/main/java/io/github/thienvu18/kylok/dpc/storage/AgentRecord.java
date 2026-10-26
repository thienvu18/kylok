// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc.storage;

import androidx.room.Entity;
import androidx.room.PrimaryKey;
import androidx.annotation.NonNull;

/** Only authenticated ciphertext is written to Room; the key lives in Android Keystore. */
@Entity(tableName = "agent_state")
public class AgentRecord {
    @PrimaryKey public int id = 1;
    @NonNull public byte[] sealed = new byte[0];
}
