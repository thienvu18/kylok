plugins {
    id("com.android.library") version "9.1.1" apply false
    id("com.android.application") version "9.1.1" apply false
    id("com.google.protobuf") version "0.10.0" apply false
    id("org.jetbrains.dokka") version "2.2.0" apply false
}

subprojects {
    dependencyLocking { lockAllConfigurations() }
}
