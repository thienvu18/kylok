pluginManagement {
    repositories { google(); mavenCentral(); gradlePluginPortal() }
}
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories { google(); mavenCentral() }
}
rootProject.name = "kylok"
include(":kylok-dpc", ":minimal-dpc", ":configuration-app")
project(":minimal-dpc").projectDir = file("../examples/minimal-dpc")

project(":configuration-app").projectDir = file("../examples/configuration-app")
