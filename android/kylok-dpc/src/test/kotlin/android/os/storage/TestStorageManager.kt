// SPDX-License-Identifier: LGPL-3.0-only
package android.os.storage

import java.io.File
import java.util.UUID

/** Test double for StorageManager. */
class TestStorageManager : StorageManager() {
    var allocatableBytes: Long = 1024L * 1024 * 1024
    val defaultUuid: UUID = UUID.randomUUID()

    override fun getUuidForPath(path: File): UUID = defaultUuid
    override fun getAllocatableBytes(storageUuid: UUID): Long = allocatableBytes
}
