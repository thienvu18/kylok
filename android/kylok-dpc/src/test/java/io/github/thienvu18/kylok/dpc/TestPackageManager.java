// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc;

import android.content.ComponentName;
import android.content.Intent;
import android.content.IntentFilter;
import android.content.pm.*;
import android.content.res.AssetManager;
import android.content.res.Resources;
import android.content.res.XmlResourceParser;
import android.graphics.Rect;
import android.graphics.drawable.Drawable;
import android.os.UserHandle;

import java.util.*;

/** Test double for PackageManager with configurable packages, permissions, and installer. */
public class TestPackageManager extends PackageManager {
    public final List<PackageInfo> installedPackages = new ArrayList<>();
    public final Map<String, PackageInfo> packageInfoMap = new HashMap<>();
    public final Map<String, PermissionInfo> permissionInfoMap = new HashMap<>();
    public final Map<String, Integer> permissionGrants = new HashMap<>();
    public final Map<String, Intent> launchIntentMap = new HashMap<>();
    public final Map<ComponentName, Integer> componentEnabledSettingMap = new HashMap<>();
    public final Map<String, PackageInfo> packageArchiveInfoMap = new HashMap<>();
    public final TestPackageInstaller testInstaller = new TestPackageInstaller();

    public java.util.function.BiFunction<Intent, Integer, ResolveInfo> resolveActivityHandler = null;
    public java.util.function.BiFunction<Intent, Integer, List<ResolveInfo>> queryIntentActivitiesHandler = null;
    public java.util.function.BiFunction<String, Integer, Boolean> hasSystemFeatureHandler = null;

    @Override
    public PackageInfo getPackageInfo(String packageName, int flags) throws NameNotFoundException {
        PackageInfo info = packageInfoMap.get(packageName);
        if (info == null) throw new NameNotFoundException(packageName);
        return info;
    }

    @Override
    public PackageInfo getPackageInfo(String packageName, PackageInfoFlags flags) throws NameNotFoundException {
        PackageInfo info = packageInfoMap.get(packageName);
        if (info == null) throw new NameNotFoundException(packageName);
        return info;
    }

    @Override
    public PackageInfo getPackageInfo(VersionedPackage versionedPackage, int flags) throws NameNotFoundException {
        return getPackageInfo(versionedPackage.getPackageName(), flags);
    }

    @Override
    public PackageInfo getPackageInfo(VersionedPackage versionedPackage, PackageInfoFlags flags) throws NameNotFoundException {
        return getPackageInfo(versionedPackage.getPackageName(), flags);
    }

    @Override
    public PackageInfo getPackageArchiveInfo(String archiveFilePath, int flags) {
        return packageArchiveInfoMap.get(archiveFilePath);
    }

    @Override
    public PackageInfo getPackageArchiveInfo(String archiveFilePath, PackageInfoFlags flags) {
        return packageArchiveInfoMap.get(archiveFilePath);
    }

    @Override
    public PermissionInfo getPermissionInfo(String permName, int flags) throws NameNotFoundException {
        PermissionInfo info = permissionInfoMap.get(permName);
        if (info == null) throw new NameNotFoundException(permName);
        return info;
    }

    @Override
    public List<PackageInfo> getInstalledPackages(int flags) {
        return installedPackages;
    }

    @Override
    public List<PackageInfo> getInstalledPackages(PackageInfoFlags flags) {
        return installedPackages;
    }

    @Override
    public int checkPermission(String permName, String pkgName) {
        Integer res = permissionGrants.get(pkgName + ":" + permName);
        return res != null ? res : PERMISSION_DENIED;
    }

    @Override
    public Intent getLaunchIntentForPackage(String packageName) {
        return launchIntentMap.get(packageName);
    }

    @Override
    public ResolveInfo resolveActivity(Intent intent, int flags) {
        return resolveActivityHandler != null ? resolveActivityHandler.apply(intent, flags) : null;
    }

    @Override
    public List<ResolveInfo> queryIntentActivities(Intent intent, int flags) {
        return queryIntentActivitiesHandler != null ? queryIntentActivitiesHandler.apply(intent, flags) : Collections.emptyList();
    }

    @Override
    public void setComponentEnabledSetting(ComponentName componentName, int newState, int flags) {
        componentEnabledSettingMap.put(componentName, newState);
    }

    @Override
    public int getComponentEnabledSetting(ComponentName componentName) {
        Integer res = componentEnabledSettingMap.get(componentName);
        return res != null ? res : COMPONENT_ENABLED_STATE_DEFAULT;
    }

    @Override
    public boolean hasSystemFeature(String name) {
        return hasSystemFeature(name, 0);
    }

    @Override
    public boolean hasSystemFeature(String name, int version) {
        return hasSystemFeatureHandler != null ? hasSystemFeatureHandler.apply(name, version) : false;
    }

    @Override
    public PackageInstaller getPackageInstaller() {
        return testInstaller;
    }

    @Override public String[] currentToCanonicalPackageNames(String[] names) { return new String[0]; }
    @Override public String[] canonicalToCurrentPackageNames(String[] names) { return new String[0]; }
    @Override public Intent getLeanbackLaunchIntentForPackage(String packageName) { return null; }
    @Override public int[] getPackageGids(String packageName) { return new int[0]; }
    @Override public int[] getPackageGids(String packageName, int flags) { return new int[0]; }
    @Override public int getPackageUid(String packageName, int flags) { return 0; }
    @Override public List<PermissionInfo> queryPermissionsByGroup(String permissionGroup, int flags) { return Collections.emptyList(); }
    @Override public PermissionGroupInfo getPermissionGroupInfo(String name, int flags) throws NameNotFoundException { throw new NameNotFoundException(name); }
    @Override public List<PermissionGroupInfo> getAllPermissionGroups(int flags) { return Collections.emptyList(); }
    @Override public ApplicationInfo getApplicationInfo(String packageName, int flags) throws NameNotFoundException {
        PackageInfo pi = packageInfoMap.get(packageName);
        if (pi != null && pi.applicationInfo != null) return pi.applicationInfo;
        throw new NameNotFoundException(packageName);
    }
    @Override public ActivityInfo getActivityInfo(ComponentName component, int flags) throws NameNotFoundException { throw new NameNotFoundException(component.flattenToString()); }
    @Override public ActivityInfo getReceiverInfo(ComponentName component, int flags) throws NameNotFoundException { throw new NameNotFoundException(component.flattenToString()); }
    @Override public ServiceInfo getServiceInfo(ComponentName component, int flags) throws NameNotFoundException { throw new NameNotFoundException(component.flattenToString()); }
    @Override public ProviderInfo getProviderInfo(ComponentName component, int flags) throws NameNotFoundException { throw new NameNotFoundException(component.flattenToString()); }
    @Override public List<PackageInfo> getPackagesHoldingPermissions(String[] permissions, int flags) { return Collections.emptyList(); }
    @Override public boolean isPermissionRevokedByPolicy(String permName, String pkgName) { return false; }
    @Override public boolean addPermission(PermissionInfo info) { return false; }
    @Override public boolean addPermissionAsync(PermissionInfo info) { return false; }
    @Override public void removePermission(String name) {}
    @Override public int checkSignatures(String pkg1, String pkg2) { return SIGNATURE_UNKNOWN_PACKAGE; }
    @Override public int checkSignatures(int uid1, int uid2) { return SIGNATURE_UNKNOWN_PACKAGE; }
    @Override public String[] getPackagesForUid(int uid) { return null; }
    @Override public String getNameForUid(int uid) { return null; }
    @Override public List<ApplicationInfo> getInstalledApplications(int flags) { return Collections.emptyList(); }
    @Override public boolean isInstantApp() { return false; }
    @Override public boolean isInstantApp(String packageName) { return false; }
    @Override public int getInstantAppCookieMaxBytes() { return 0; }
    @Override public byte[] getInstantAppCookie() { return new byte[0]; }
    @Override public void clearInstantAppCookie() {}
    @Override public void updateInstantAppCookie(byte[] cookie) {}
    @Override public String[] getSystemSharedLibraryNames() { return null; }
    @Override public List<SharedLibraryInfo> getSharedLibraries(int flags) { return Collections.emptyList(); }
    @Override public ChangedPackages getChangedPackages(int sequenceNumber) { return null; }
    @Override public FeatureInfo[] getSystemAvailableFeatures() { return new FeatureInfo[0]; }
    @Override public List<ResolveInfo> queryIntentActivityOptions(ComponentName caller, Intent[] specifics, Intent intent, int flags) { return Collections.emptyList(); }
    @Override public List<ResolveInfo> queryBroadcastReceivers(Intent intent, int flags) { return Collections.emptyList(); }
    @Override public ResolveInfo resolveService(Intent intent, int flags) { return null; }
    @Override public List<ResolveInfo> queryIntentServices(Intent intent, int flags) { return Collections.emptyList(); }
    @Override public List<ResolveInfo> queryIntentContentProviders(Intent intent, int flags) { return Collections.emptyList(); }
    @Override public List<ProviderInfo> queryContentProviders(String processName, int uid, int flags) { return Collections.emptyList(); }
    @Override public ProviderInfo resolveContentProvider(String name, int flags) { return null; }
    @Override public InstrumentationInfo getInstrumentationInfo(ComponentName className, int flags) throws NameNotFoundException { throw new NameNotFoundException(className.flattenToString()); }
    @Override public List<InstrumentationInfo> queryInstrumentation(String targetPackage, int flags) { return Collections.emptyList(); }
    @Override public Drawable getDrawable(String packageName, int resid, ApplicationInfo appInfo) { return null; }
    @Override public Drawable getActivityIcon(ComponentName activityName) { throw new UnsupportedOperationException(); }
    @Override public Drawable getActivityIcon(Intent intent) { throw new UnsupportedOperationException(); }
    @Override public Drawable getActivityBanner(ComponentName activityName) { return null; }
    @Override public Drawable getActivityBanner(Intent intent) { return null; }
    @Override public Drawable getDefaultActivityIcon() { throw new UnsupportedOperationException(); }
    @Override public Drawable getApplicationIcon(ApplicationInfo info) { throw new UnsupportedOperationException(); }
    @Override public Drawable getApplicationIcon(String packageName) { throw new UnsupportedOperationException(); }
    @Override public Drawable getApplicationBanner(ApplicationInfo info) { return null; }
    @Override public Drawable getApplicationBanner(String packageName) { return null; }
    @Override public Drawable getActivityLogo(ComponentName activityName) { return null; }
    @Override public Drawable getActivityLogo(Intent intent) { return null; }
    @Override public Drawable getApplicationLogo(ApplicationInfo info) { return null; }
    @Override public Drawable getApplicationLogo(String packageName) { return null; }
    @Override public Drawable getUserBadgedIcon(Drawable drawable, UserHandle user) { return drawable; }
    @Override public Drawable getUserBadgedDrawableForDensity(Drawable drawable, UserHandle user, Rect badgeLocation, int density) { return drawable; }
    @Override public CharSequence getUserBadgedLabel(CharSequence label, UserHandle user) { return label; }
    @Override public CharSequence getText(String packageName, int resid, ApplicationInfo appInfo) { return null; }
    @Override public XmlResourceParser getXml(String packageName, int resid, ApplicationInfo appInfo) { return null; }
    @Override public CharSequence getApplicationLabel(ApplicationInfo info) { return info.packageName; }
    @Override public Resources getResourcesForActivity(ComponentName activityName) { throw new UnsupportedOperationException(); }
    @Override public Resources getResourcesForApplication(ApplicationInfo app) { throw new UnsupportedOperationException(); }
    @Override public Resources getResourcesForApplication(String packageName) { throw new UnsupportedOperationException(); }
    @Override public void verifyPendingInstall(int id, int verificationCode) {}
    @Override public void extendVerificationTimeout(int id, int verificationCodeAtTimeout, long millisecondsToDelay) {}
    @Override public void setInstallerPackageName(String targetPackage, String installerPackageName) {}
    @Override public void setApplicationCategoryHint(String packageName, int categoryHint) {}
    @Override public String getInstallerPackageName(String packageName) { return null; }
    @Override public void addPackageToPreferred(String packageName) {}
    @Override public void removePackageFromPreferred(String packageName) {}
    @Override public List<PackageInfo> getPreferredPackages(int flags) { return Collections.emptyList(); }
    @Override public void addPreferredActivity(IntentFilter filter, int match, ComponentName[] set, ComponentName activity) {}
    @Override public void clearPackagePreferredActivities(String packageName) {}
    @Override public int getPreferredActivities(List<IntentFilter> outFilters, List<ComponentName> outActivities, String packageName) { return 0; }
    @Override public void setApplicationEnabledSetting(String packageName, int newState, int flags) {}
    @Override public int getApplicationEnabledSetting(String packageName) { return COMPONENT_ENABLED_STATE_DEFAULT; }
    @Override public boolean isSafeMode() { return false; }
    @Override public boolean canRequestPackageInstalls() { return true; }
}
