// SPDX-License-Identifier: LGPL-3.0-only
package android.app;

import android.content.Context;
import android.content.Intent;
import android.os.Bundle;
import android.view.ContextThemeWrapper;
import android.view.View;
import android.window.OnBackInvokedDispatcher;

/** Test double for Activity providing mutable intent, result and lifecycle states. */
public class Activity extends ContextThemeWrapper {
    public static final int RESULT_CANCELED = 0;
    public static final int RESULT_OK = -1;

    private Intent mIntent;
    public int mResultCode = RESULT_CANCELED;
    public Intent mResultData;
    public boolean mFinished = false;
    public View mContentView;
    public int mRequestedPermissionCode = -1;
    public String[] mRequestedPermissions;
    public boolean mLockTaskStopped = false;

    public Activity() {
        super();
    }

    private Context mBaseContext;

    public void attach(Context base) {
        this.mBaseContext = base;
        attachBaseContext(base);
    }

    @Override
    public Context getBaseContext() {
        return mBaseContext != null ? mBaseContext : super.getBaseContext();
    }

    @Override
    public android.content.pm.PackageManager getPackageManager() {
        return getBaseContext() != null ? getBaseContext().getPackageManager() : null;
    }

    @Override
    public Object getSystemService(String name) {
        return getBaseContext() != null ? getBaseContext().getSystemService(name) : null;
    }

    @Override
    public String getPackageName() {
        return getBaseContext() != null ? getBaseContext().getPackageName() : "";
    }

    @Override
    public int checkSelfPermission(String permission) {
        return getBaseContext() != null ? getBaseContext().checkSelfPermission(permission) : android.content.pm.PackageManager.PERMISSION_GRANTED;
    }

    @Override
    public Context getApplicationContext() {
        return getBaseContext() != null ? getBaseContext().getApplicationContext() : this;
    }

    public Intent getIntent() {
        if (mIntent == null) {
            mIntent = new Intent();
        }
        return mIntent;
    }
    public void setIntent(Intent newIntent) { mIntent = newIntent; }

    public final void setResult(int resultCode) { mResultCode = resultCode; }
    public final void setResult(int resultCode, Intent data) {
        mResultCode = resultCode;
        mResultData = data;
    }

    public void finish() { mFinished = true; }
    public boolean isFinishing() { return mFinished; }

    public void setContentView(View view) { mContentView = view; }
    public void setContentView(int layoutResID) {}

    public void requestPermissions(String[] permissions, int requestCode) {
        mRequestedPermissions = permissions;
        mRequestedPermissionCode = requestCode;
    }

    public void onRequestPermissionsResult(int requestCode, String[] permissions, int[] grantResults) {}

    public void stopLockTask() { mLockTaskStopped = true; }

    public OnBackInvokedDispatcher getOnBackInvokedDispatcher() {
        return new OnBackInvokedDispatcher() {
            @Override
            public void registerOnBackInvokedCallback(int priority, android.window.OnBackInvokedCallback callback) {}
            @Override
            public void unregisterOnBackInvokedCallback(android.window.OnBackInvokedCallback callback) {}
        };
    }

    public void onBackPressed() {}

    public void onCreate(Bundle savedInstanceState) {}
    public void onResume() {}
    public void onDestroy() {}
    public void onNewIntent(Intent intent) {}
}
