plugins {
    id("com.android.application")
}

android {
    namespace = "com.efe.droidmcp"
    compileSdk = 36

    defaultConfig {
        applicationId = "com.efe.droidmcp"
        minSdk = 30
        targetSdk = 36
        versionCode = 1
        versionName = "0.1.0"
    }

    buildTypes {
        release {
            // The helper is started by class name from app_process, so keep code un-shrunk.
            isMinifyEnabled = false
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}
