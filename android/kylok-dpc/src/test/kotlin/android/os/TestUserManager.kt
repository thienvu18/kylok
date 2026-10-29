// SPDX-License-Identifier: LGPL-3.0-only
package android.os

/** Test double for UserManager. */
class TestUserManager : UserManager() {
    val restrictions = mutableSetOf<String>()
    override fun hasUserRestriction(restrictionKey: String?): Boolean =
        restrictionKey != null && restrictions.contains(restrictionKey)
}
