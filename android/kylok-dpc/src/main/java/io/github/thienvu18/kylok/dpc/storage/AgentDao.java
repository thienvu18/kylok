// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc.storage;

import androidx.room.Dao;
import androidx.room.Insert;
import androidx.room.OnConflictStrategy;
import androidx.room.Query;

@Dao
public interface AgentDao {
    /** Load the sole encrypted checkpoint. Usage: Room calls the generated DAO through AgentStorage. */
    @Query("SELECT * FROM agent_state WHERE id = 1") AgentRecord load();
    /** Replace the complete encrypted checkpoint atomically. Usage: AgentStorage saves inside a Room transaction. */
    @Insert(onConflict = OnConflictStrategy.REPLACE) void save(AgentRecord record);
}
