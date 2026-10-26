pluginManagement { repositories { google(); mavenCentral(); gradlePluginPortal() } }
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        maven {
            url = uri(requireNotNull(System.getenv("KYLOK_MAVEN_REPO")))
            content { includeGroup("io.github.thienvu18.kylok") }
        }
        google(); mavenCentral()
    }
}
rootProject.name = "fresh-kylok-host"
