// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import androidx.room.DatabaseConfiguration
import androidx.room.InvalidationTracker
import androidx.sqlite.db.SupportSQLiteOpenHelper
import io.github.thienvu18.kylok.dpc.storage.AgentDao
import io.github.thienvu18.kylok.dpc.storage.AgentDatabase
import io.github.thienvu18.kylok.dpc.storage.AgentRecord
import java.util.concurrent.atomic.AtomicReference

/** In-memory test implementation of AgentDatabase. */
class TestAgentDatabase : AgentDatabase() {
    val recordRef = AtomicReference<AgentRecord?>(null)

    private val dao = object : AgentDao {
        override fun load(): AgentRecord? = recordRef.get()
        override fun save(record: AgentRecord) {
            recordRef.set(record)
        }
    }

    override fun agent(): AgentDao = dao

    override fun runInTransaction(body: Runnable) {
        body.run()
    }

    override fun beginTransaction() {}
    override fun endTransaction() {}
    override fun setTransactionSuccessful() {}
    override fun inTransaction(): Boolean = true
    override fun assertNotMainThread() {}

    override fun createOpenHelper(config: DatabaseConfiguration): SupportSQLiteOpenHelper {
        throw UnsupportedOperationException("In-memory test double")
    }

    override fun createInvalidationTracker(): InvalidationTracker {
        return object : InvalidationTracker(this@TestAgentDatabase, "agent_state") {}
    }

    override fun clearAllTables() {
        recordRef.set(null)
    }
}
