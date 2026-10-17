import java.util.Properties

plugins {
    id("com.android.application")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

repositories {
    maven {
        url = uri("https://jitpack.io")
        content { includeGroup("cz.adaptech.tesseract4android") }
    }
}

dependencies {
    // Bundled, single-threaded local OCR. No cloud or runtime model download.
    implementation("cz.adaptech.tesseract4android:tesseract4android:4.9.0")
}

val amapProperties = Properties()
val amapPropertiesFile = rootProject.file("amap.properties")
if (amapPropertiesFile.exists()) {
    amapPropertiesFile.inputStream().use { amapProperties.load(it) }
}

val releaseBuildRequested = gradle.startParameter.taskNames.any { it.contains("release", ignoreCase = true) }
// A separately installed, loopback-backed acceptance build keeps the user's
// normal preview data and authentication intact. Never change the store ID.
val isolatedAcceptanceApp = System.getenv("BIRDTIE_ANDROID_ACCEPTANCE_APP") == "1"
if (releaseBuildRequested && isolatedAcceptanceApp) {
    throw GradleException("The isolated acceptance identity is unavailable for release builds")
}
val previewSuffix = if (isolatedAcceptanceApp) ".birdtieacceptance" else ".birdtiepreview"
val releaseStorePath = System.getenv("BIRDTIE_ANDROID_STORE_FILE")
val releaseAlias = System.getenv("BIRDTIE_ANDROID_KEY_ALIAS")
val releaseStorePassword = System.getenv("BIRDTIE_ANDROID_STORE_PASSWORD")
val releaseKeyPassword = System.getenv("BIRDTIE_ANDROID_KEY_PASSWORD")
val releaseSigningReady = listOf(
    releaseStorePath,
    releaseAlias,
    releaseStorePassword,
    releaseKeyPassword,
).all { !it.isNullOrBlank() }

if (releaseBuildRequested && !releaseSigningReady) {
    throw GradleException("Release build requires the original Civu signing identity via BIRDTIE_ANDROID_* environment variables")
}
if (releaseBuildRequested && !file(releaseStorePath!!).isFile) {
    throw GradleException("BIRDTIE_ANDROID_STORE_FILE must point to an existing keystore")
}

android {
    namespace = "app.civu.civu_mobile"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    buildFeatures {
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        // Keep the installed Civu application's identity for the eventual upgrade.
        applicationId = "app.civu.civu_mobile"
        // You can update the following values to match your application needs.
        // For more information, see: https://flutter.dev/to/review-gradle-config.
        minSdk = flutter.minSdkVersion
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
        if (releaseBuildRequested && flutter.versionCode <= 2038) {
            throw GradleException("Release versionCode must exceed the installed Civu baseline 2038; also check every Play track before publishing")
        }
        manifestPlaceholders["AMAP_ANDROID_KEY"] = amapProperties.getProperty("android.key", "")
        externalNativeBuild {
            cmake {
                // This JNI boundary owns its C++ objects; avoid a new shared
                // STL package alongside existing map/OCR native libraries.
                arguments += "-DANDROID_STL=c++_static"
            }
        }
    }

    externalNativeBuild {
        cmake {
            path = file("src/main/cpp/birdtie_asr/CMakeLists.txt")
            version = "3.22.1"
        }
    }

    signingConfigs {
        if (releaseSigningReady) {
            create("civuUpgrade") {
                storeFile = file(releaseStorePath!!)
                keyAlias = releaseAlias
                storePassword = releaseStorePassword
                keyPassword = releaseKeyPassword
            }
        }
    }

    buildTypes {
        debug {
            // Side-by-side phone testing must not replace the installed Civu app.
            applicationIdSuffix = previewSuffix
        }
        getByName("profile") {
            // Flutter creates profile from debug before the project adds its
            // suffix. Keep physical profiling on the same isolated preview ID.
            applicationIdSuffix = previewSuffix
        }
        release {
            if (releaseSigningReady) {
                signingConfig = signingConfigs.getByName("civuUpgrade")
            }
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

flutter {
    source = "../.."
}

dependencies {
    // Same artifact already used by the CIVU-derived map plugin. Expose its
    // existing search SDK to the app's narrow geographic-search bridge.
    implementation("com.amap.api:3dmap-location-search:11.2.100_loc11.2.100_sea9.8.1")
}
