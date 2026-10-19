plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
}

/** Program icons from the Mac app's own sources (script/program-icons.py): one truth for both apps. */
abstract class ProgramIconsTask : DefaultTask() {
    @get:InputFile abstract val script: RegularFileProperty
    @get:InputFiles abstract val sources: ConfigurableFileCollection
    @get:Internal abstract val macSources: DirectoryProperty
    @get:OutputDirectory abstract val outputDir: DirectoryProperty

    @TaskAction
    fun run() {
        val out = outputDir.get().asFile.also { it.mkdirs() }
        val p = ProcessBuilder("python3", script.get().asFile.path, macSources.get().asFile.path, File(out, "programs.json").path)
            .redirectErrorStream(true).start()
        val said = p.inputStream.bufferedReader().readText().trim()
        check(p.waitFor() == 0) { "program-icons.py failed: $said" }
        logger.lifecycle(said)
    }
}

// The repository root: libghostty-vt (make vt-android), the Mac app's C shim
// and its terminal fonts are shared from there.
val root = rootDir.parentFile.parentFile

android {
    namespace = "xyz.matm.hive"
    compileSdk = 37
    ndkVersion = "28.2.13676358"

    defaultConfig {
        applicationId = "xyz.matm.hive"
        minSdk = 29
        targetSdk = 37
        versionCode = 5
        versionName = "0.1.4"
        ndk { abiFilters += "arm64-v8a" }
        externalNativeBuild {
            cmake { arguments += listOf("-DHIVE_ROOT=${root.absolutePath}", "-DANDROID_SUPPORT_FLEXIBLE_PAGE_SIZES=ON") }
        }
    }

    externalNativeBuild {
        cmake {
            path = file("src/main/cpp/CMakeLists.txt")
            version = "3.22.1"
        }
    }

    sourceSets {
        getByName("main") {
            assets.srcDir("$root/apps/macos/Sources/HiveRender/Resources/fonts")
        }
    }

    // The release key lives in ~/.android-keys; its password comes from the macOS Keychain at build time:
    // HIVE_RELEASE_PW=$(security find-generic-password -s hive-release-keystore -w) ./gradlew assembleRelease
    val releasePw = System.getenv("HIVE_RELEASE_PW")
    signingConfigs {
        if (releasePw != null) create("release") {
            storeFile = file("${System.getProperty("user.home")}/.android-keys/hive-release.jks")
            storePassword = releasePw
            keyAlias = "hive-release"
            keyPassword = releasePw
        }
    }

    buildTypes {
        debug {
            applicationIdSuffix = ".debug"
            // Beside the release on a test phone: "hive dev".
            resValue("string", "app_name", "hive dev")
        }
        release {
            resValue("string", "app_name", "hive")
            signingConfigs.findByName("release")?.let { signingConfig = it }
            isMinifyEnabled = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_21
        targetCompatibility = JavaVersion.VERSION_21
    }
    buildFeatures {
        compose = true
        resValues = true
    }
    testOptions {
        unitTests.isReturnDefaultValues = true
    }
}

val programIcons = tasks.register<ProgramIconsTask>("generateProgramIcons") {
    val core = File(root, "apps/macos/Sources/HiveCore")
    script.set(file("../script/program-icons.py"))
    macSources.set(File(root, "apps/macos/Sources"))
    sources.from(File(core, "Program.swift"), File(core, "Icons.generated.swift"), File(core, "VendorIcons.generated.swift"))
    outputDir.set(layout.buildDirectory.dir("generated/programs"))
}

androidComponents {
    onVariants { v -> v.sources.assets?.addGeneratedSourceDirectory(programIcons, ProgramIconsTask::outputDir) }
}

// Unit tests read the same generated table.
tasks.withType<Test>().configureEach {
    dependsOn(programIcons)
    systemProperty("hive.programs", layout.buildDirectory.file("generated/programs/programs.json").get().asFile.path)
}

dependencies {
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.foundation)
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.camera.camera2)
    implementation(libs.androidx.camera.lifecycle)
    implementation(libs.androidx.camera.view)
    implementation(libs.zxing.core)
    testImplementation(libs.junit)
    testImplementation(libs.org.json)
}
