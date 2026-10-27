// SPDX-License-Identifier: LGPL-3.0-only
plugins { id("com.android.application") }
android {
    namespace = "dev.kylok.configuration"
    compileSdk = 37
    defaultConfig {
        applicationId = "dev.kylok.configuration"; minSdk = 33; targetSdk = 37
        // Preserve the package/signer while building distinct APKs for physical update/downgrade checks.
        val fixtureVersion = providers.gradleProperty("configurationVersionCode").orNull?.toInt() ?: 1
        require(fixtureVersion > 0) { "configurationVersionCode must be positive" }
        versionCode = fixtureVersion
        versionName = "1.0"
    }
    compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
}
