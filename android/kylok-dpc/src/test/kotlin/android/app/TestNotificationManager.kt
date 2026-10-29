// SPDX-License-Identifier: LGPL-3.0-only
package android.app

/** Test double for NotificationManager. */
class TestNotificationManager : NotificationManager() {
    val channels = mutableListOf<NotificationChannel>()
    val notifications = mutableMapOf<Int, Notification>()

    override fun createNotificationChannel(channel: NotificationChannel) {
        channels.add(channel)
    }

    override fun notify(id: Int, notification: Notification) {
        notifications[id] = notification
    }
}
