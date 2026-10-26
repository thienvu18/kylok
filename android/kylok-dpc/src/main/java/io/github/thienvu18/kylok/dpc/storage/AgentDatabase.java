// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc.storage;

import androidx.room.Database;
import androidx.room.RoomDatabase;

@Database(entities = {AgentRecord.class}, version = 1, exportSchema = false)
public abstract class AgentDatabase extends RoomDatabase {
    /** Access the encrypted-state DAO. Usage: internal AgentStorage transactions call database.agent(). */
    public abstract AgentDao agent();
}
