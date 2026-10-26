import java.security.MessageDigest

plugins { id("com.android.application") version "9.1.1" }

android {
    namespace = "dev.kylok.consumer"
    compileSdk = 37
    buildToolsVersion = "37.0.0"
    defaultConfig { applicationId = "dev.kylok.consumer"; minSdk = 33; targetSdk = 37; versionCode = 1; versionName = "consumer-test" }
    compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
    buildTypes {
        getByName("release") {
            isMinifyEnabled = true
            signingConfig = signingConfigs.getByName("debug")
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"))
        }
    }
}

dependencies { implementation("io.github.thienvu18.kylok:kylok-dpc:${requireNotNull(System.getenv("KYLOK_ANDROID_VERSION"))}") }

val sdkArtifact = configurations.detachedConfiguration(dependencies.create(
    "io.github.thienvu18.kylok:kylok-dpc:${requireNotNull(System.getenv("KYLOK_ANDROID_VERSION"))}@aar")).apply { isTransitive = false }
val expectedDigest = requireNotNull(System.getenv("KYLOK_AAR_SHA256"))
tasks.register("verifySdkArtifact") {
    inputs.files(sdkArtifact)
    doLast {
        val actual = MessageDigest.getInstance("SHA-256").digest(inputs.files.singleFile.readBytes())
            .joinToString("") { "%02x".format(it) }
        require(actual == expectedDigest) { "Resolved AAR differs from the verified distribution" }
    }
}
