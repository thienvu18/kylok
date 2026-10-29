// SPDX-License-Identifier: LGPL-3.0-only
package androidx.work;

import android.content.Context;
import java.util.HashMap;
import java.util.Map;

/** Test double for WorkManager enabling deterministic unit testing of scheduled work. */
public abstract class WorkManager {
    private static WorkManager sInstance;

    public static final Companion Companion = new Companion();

    public static class Companion {
        public WorkManager getInstance(Context context) {
            return WorkManager.getInstance(context);
        }
        public void initialize(Context context, Configuration configuration) {
            WorkManager.initialize(context, configuration);
        }
    }

    public static WorkManager getInstance(Context context) {
        if (sInstance == null) {
            sInstance = new TestWorkManager();
        }
        return sInstance;
    }

    public static void initialize(Context context, Configuration configuration) {
        if (sInstance == null) {
            sInstance = new TestWorkManager();
        }
    }

    public abstract Operation enqueue(WorkRequest workRequest);
    public abstract Operation enqueueUniqueWork(String uniqueWorkName, ExistingWorkPolicy existingWorkPolicy, OneTimeWorkRequest work);
    public abstract Operation enqueueUniquePeriodicWork(String uniqueWorkName, ExistingPeriodicWorkPolicy existingPeriodicWorkPolicy, PeriodicWorkRequest periodicWork);
    public abstract Operation cancelUniqueWork(String uniqueWorkName);

    public static class TestWorkManager extends WorkManager {
        public final Map<String, WorkRequest> enqueued = new HashMap<>();

        @Override
        public Operation enqueue(WorkRequest workRequest) {
            return new TestOperation();
        }

        @Override
        public Operation enqueueUniqueWork(String uniqueWorkName, ExistingWorkPolicy existingWorkPolicy, OneTimeWorkRequest work) {
            enqueued.put(uniqueWorkName, work);
            return new TestOperation();
        }

        @Override
        public Operation enqueueUniquePeriodicWork(String uniqueWorkName, ExistingPeriodicWorkPolicy existingPeriodicWorkPolicy, PeriodicWorkRequest periodicWork) {
            enqueued.put(uniqueWorkName, periodicWork);
            return new TestOperation();
        }

        @Override
        public Operation cancelUniqueWork(String uniqueWorkName) {
            enqueued.remove(uniqueWorkName);
            return new TestOperation();
        }
    }

    public static class TestOperation implements Operation {
        @Override
        public com.google.common.util.concurrent.ListenableFuture<Operation.State.SUCCESS> getResult() {
            return androidx.work.impl.utils.futures.SettableFuture.create();
        }

        @Override
        public androidx.lifecycle.LiveData<Operation.State> getState() {
            return new androidx.lifecycle.MutableLiveData<>();
        }
    }
}
