import com.google.protobuf.gradle.*

abstract class LibraryNotices : DefaultTask() {
    @get:InputFiles @get:PathSensitive(PathSensitivity.NONE)
    abstract val notices: ConfigurableFileCollection
    @get:OutputDirectory abstract val outputDir: DirectoryProperty
    /** Copy required notices into reproducible library resources.
     * Usage: Gradle invokes prepareLibraryNotices before binary/source packaging.
     */
    @TaskAction fun generate() {
        val directory = outputDir.get().asFile.resolve("META-INF/kylok")
        directory.mkdirs()
        notices.files.forEach { it.copyTo(directory.resolve(it.name), overwrite = true) }
    }
}

plugins {
    id("com.android.library")
    id("com.google.protobuf")
    id("maven-publish")
    id("org.jetbrains.dokka")
}

group = "io.github.thienvu18.kylok"
version = rootProject.file("VERSION").readText().trim()
require(version.toString().matches(Regex("(?:0|[1-9][0-9]*)\\.(?:0|[1-9][0-9]*)\\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\\.[0-9A-Za-z-]+)*)?")) &&
    version.toString().substringAfter('-', "").split('.').none { it.length > 1 && it.all(Char::isDigit) && it.startsWith('0') }) { "Set android/VERSION to a supported SemVer" }

android {
    namespace = "io.github.thienvu18.kylok.dpc"
    compileSdk = 37
    buildToolsVersion = "37.0.0"
    defaultConfig {
        minSdk = 33
        consumerProguardFiles("consumer-rules.pro")
        buildConfigField("String", "SDK_VERSION", "\"${project.version}\"")
        buildConfigField("int", "PROTOCOL_VERSION", "1")
    }
    buildFeatures { buildConfig = true }
    buildTypes {
        debug {
            enableUnitTestCoverage = true
        }
    }
    testOptions {
        unitTests {
            isReturnDefaultValues = true
        }
    }
    publishing { singleVariant("release") { withSourcesJar() } }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    sourceSets.getByName("main").proto { srcDir("../../proto") }
}

// Keep the selected license texts with the code in both binary and source archives.
val libraryNotices = tasks.register<LibraryNotices>("prepareLibraryNotices") {
    notices.from(rootProject.file("../LICENSE"), rootProject.file("../COPYING"), rootProject.file("../NOTICE"))
    outputDir.set(layout.buildDirectory.dir("generated/notices"))
}
androidComponents.onVariants { variant ->
    variant.sources.resources?.addGeneratedSourceDirectory(libraryNotices, LibraryNotices::outputDir)
}
tasks.withType<Jar>().configureEach {
    isPreserveFileTimestamps = false
    isReproducibleFileOrder = true
    from(listOf(rootProject.file("../LICENSE"), rootProject.file("../COPYING"), rootProject.file("../NOTICE"))) { into("META-INF/kylok") }
    if (name == "sourceReleaseJar") from(rootProject.file("../proto")) { into("proto") }
}

// Central requires API documentation alongside sources; package actual generated KDoc as the javadoc artifact.
val apiDocumentation = tasks.register<Jar>("apiDocumentationJar") {
    dependsOn("dokkaGeneratePublicationHtml")
    archiveClassifier.set("javadoc")
    from(layout.buildDirectory.dir("dokka/html"))
}

publishing {
    publications {
        register<MavenPublication>("release") {
            artifactId = "kylok-dpc"
            artifact(apiDocumentation)
            pom {
                name = "Kylok Android DPC"
                description = "Experimental self-hosted Android device management SDK"
                url = "https://github.com/thienvu18/kylok"
                licenses { license { name = "GNU Lesser General Public License v3.0 only"; url = "https://www.gnu.org/licenses/lgpl-3.0.html"; distribution = "repo" } }
                developers { developer { id = "thienvu18"; name = "Kylok contributors"; url = "https://github.com/thienvu18/kylok" } }
                scm { url = "https://github.com/thienvu18/kylok"; connection = "scm:git:https://github.com/thienvu18/kylok.git"; developerConnection = "scm:git:ssh://git@github.com/thienvu18/kylok.git" }
            }
        }
    }
    repositories {
        maven {
            name = "Distribution"
            url = uri(providers.gradleProperty("kylokRepository").orElse(rootProject.layout.projectDirectory.dir("../out/maven").asFile.absolutePath))
        }
        // Central publication uploads the signed, checked local repository through its Publisher API.
    }
}
afterEvaluate { publishing.publications.named<MavenPublication>("release") { from(components["release"]) } }

// Generate the supported host API from source KDoc, excluding generated protocol and Room internals.
dokka {
    moduleName.set("kylok-dpc")
    dokkaSourceSets.configureEach {
        sourceRoots.setFrom(file("src/main/kotlin"), file("src/main/java"))
        perPackageOption { matchingRegex.set(".*\\.storage.*"); suppress.set(true) }
    }
    dokkaPublications.html { outputDirectory.set(layout.buildDirectory.dir("dokka/html")) }
}

protobuf {
    protoc { artifact = "com.google.protobuf:protoc:4.36.2" }
    plugins { create("grpc") { artifact = "io.grpc:protoc-gen-grpc-java:1.85.1" } }
    generateProtoTasks {
        all().configureEach {
            builtins { create("java") { option("lite") } }
            plugins { create("grpc") { option("lite") } }
        }
    }
}

dependencies {
    implementation("com.google.protobuf:protobuf-javalite:4.36.2")
    implementation("io.grpc:grpc-okhttp:1.85.1")
    implementation("io.grpc:grpc-protobuf-lite:1.85.1")
    implementation("io.grpc:grpc-stub:1.85.1")
    compileOnly("javax.annotation:javax.annotation-api:1.3.2")
    implementation("androidx.room:room-runtime:2.8.5")
    annotationProcessor("androidx.room:room-compiler:2.8.5")
    implementation("androidx.work:work-runtime:2.12.0")
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20260522")
}
