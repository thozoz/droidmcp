import java.util.Properties

plugins {
    id("com.android.application")
}

// Release signing secrets live in an untracked android/keystore.properties
// (see keystore.properties.example; CI writes it from repository secrets).
// Without it the release build still configures and is left unsigned.
val keystoreFile = rootProject.file("keystore.properties")
val keystore = Properties().apply { if (keystoreFile.exists()) keystoreFile.inputStream().use { load(it) } }

android {
    namespace = "com.thozoz.droidmcp"
    compileSdk = 36

    defaultConfig {
        applicationId = "com.thozoz.droidmcp"
        minSdk = 30
        targetSdk = 36
        // CI passes these from the release tag (v1.2.3 -> 1.2.3).
        versionCode = (project.findProperty("versionCode") as String?)?.toInt() ?: 1
        versionName = project.findProperty("versionName") as String? ?: "0.1.0"
    }

    signingConfigs {
        create("release") {
            if (keystoreFile.exists()) {
                storeFile = rootProject.file(keystore.getProperty("storeFile"))
                storePassword = keystore.getProperty("storePassword")
                keyAlias = keystore.getProperty("keyAlias")
                keyPassword = keystore.getProperty("keyPassword")
            }
        }
    }

    buildTypes {
        release {
            if (keystoreFile.exists()) signingConfig = signingConfigs.getByName("release")
            // The helper is started by class name from app_process, so keep code un-shrunk.
            isMinifyEnabled = false
        }
    }

    packaging {
        jniLibs {
            // The daemon ships as lib/arm64-v8a/libdroidmcpd.so; it must be extracted
            // to nativeLibraryDir (not left compressed in the APK) to be exec-able.
            useLegacyPackaging = true
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}
