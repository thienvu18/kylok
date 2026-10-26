pluginManagement {
    repositories { google(); mavenCentral(); gradlePluginPortal() }
}
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories { google(); mavenCentral() }
}
rootProject.name = "kylok"
include(":kylok-dpc", ":minimal-dpc")
project(":minimal-dpc").projectDir = file("../examples/minimal-dpc")
