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
            applicationIdSuffix = ".birdtiepreview"
        }
        getByName("profile") {
            // Flutter creates profile from debug before the project adds its
            // suffix. Keep physical profiling on the same isolated preview ID.
            applicationIdSuffix = ".birdtiepreview"
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
