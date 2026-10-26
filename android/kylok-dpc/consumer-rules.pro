# SPDX-License-Identifier: LGPL-3.0-only
# Room loads this generated implementation by its database's name.
-keepnames class io.github.thienvu18.kylok.dpc.storage.AgentDatabase
-keep class io.github.thienvu18.kylok.dpc.storage.AgentDatabase_Impl { <init>(); }
# WorkManager persists worker class names across host/DPC replacement.
-keep class io.github.thienvu18.kylok.dpc.EnrollmentWorker { public <init>(android.content.Context, androidx.work.WorkerParameters); }
