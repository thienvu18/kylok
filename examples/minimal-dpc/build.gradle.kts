import java.security.cert.CertificateFactory
import java.security.cert.X509Certificate
import java.net.URI
import java.io.File

plugins { id("com.android.application") }

abstract class GenerateCustomTrust : DefaultTask() {
    @get:InputFile
    @get:PathSensitive(PathSensitivity.NONE)
    abstract val caFile: RegularFileProperty
    @get:Input abstract val serverHost: Property<String>
    @get:OutputDirectory abstract val outputDir: DirectoryProperty

    /** Generate host-scoped CA trust only for the explicit customCA variant.
     * Usage: Gradle invokes this task when that variant is built with KYLOK_CA_CERT and KYLOK_TRUST_HOST.
     */
    @TaskAction fun generate() {
        val host = serverHost.get()
        require(host.matches(Regex("[A-Za-z0-9.-]+")) && URI("https://$host").host == host) {
            "Custom CA trust requires an IP address or DNS hostname without a scheme, port or path"
        }
        val certificates = caFile.get().asFile.inputStream().use {
            CertificateFactory.getInstance("X.509").generateCertificates(it)
        }
        require(certificates.size == 1 && (certificates.first() as X509Certificate).basicConstraints >= 0) {
            "KYLOK_CA_CERT must contain exactly one CA certificate"
        }
        (certificates.first() as X509Certificate).checkValidity()
        val out = outputDir.get().asFile
        out.resolve("raw").mkdirs()
        out.resolve("xml").mkdirs()
        caFile.get().asFile.copyTo(out.resolve("raw/kylok_custom_ca.pem"), overwrite = true)
        out.resolve("xml/kylok_custom_network_security.xml").writeText("""
            <network-security-config>
                <base-config cleartextTrafficPermitted="true" />
                <domain-config cleartextTrafficPermitted="true">
                    <domain includeSubdomains="false">$host</domain>
                    <trust-anchors><certificates src="@raw/kylok_custom_ca" /></trust-anchors>
                </domain-config>
            </network-security-config>
        """.trimIndent())
    }
}

val generateCustomTrust = tasks.register<GenerateCustomTrust>("generateCustomCATrust") {
    caFile.set(layout.file(providers.environmentVariable("KYLOK_CA_CERT").map { File(it) }))
    serverHost.set(providers.environmentVariable("KYLOK_TRUST_HOST"))
    outputDir.set(layout.buildDirectory.dir("generated/customCATrust/res"))
}

android {
    namespace = "io.github.thienvu18.kylok.example"
    compileSdk = 37
    buildToolsVersion = "37.0.0"
    defaultConfig {
        applicationId = "io.github.thienvu18.kylok.example"
        minSdk = 33
        targetSdk = 37
        versionCode = providers.gradleProperty("hostVersionCode").orNull?.toInt() ?: 1
        versionName = "0.0.1-dev"
    }
    signingConfigs {
        create("host") {
            // Secrets and key location belong to the host, outside version control.
            val path = providers.environmentVariable("KYLOK_KEYSTORE").orNull
            if (path != null) {
                storeFile = file(path)
                storePassword = providers.environmentVariable("KYLOK_STORE_PASSWORD").orNull
                keyAlias = providers.environmentVariable("KYLOK_KEY_ALIAS").orNull
                keyPassword = providers.environmentVariable("KYLOK_KEY_PASSWORD").orNull
            }
        }
    }
    buildTypes {
        getByName("release") { signingConfig = signingConfigs.getByName("host") }
        create("customCA") {
            initWith(getByName("release"))
            signingConfig = signingConfigs.getByName("host")
            matchingFallbacks += listOf("release")
            versionNameSuffix = "-custom-ca"
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

androidComponents {
    onVariants(selector().withBuildType("customCA")) { variant ->
        variant.sources.res?.addGeneratedSourceDirectory(generateCustomTrust, GenerateCustomTrust::outputDir)
    }
}

dependencies { implementation(project(":kylok-dpc")) }
