// app/build.gradle.kts — Jarvis Android Client
//
// Proto codegen note:
//   The protobuf plugin generates Kotlin + gRPC stubs from the shared
//   proto/voice/voice.proto and proto/common/common.proto files in the
//   monorepo root.  The sourceSets block below points the plugin at those
//   files so `./gradlew generateDebugProto` or just `./gradlew build`
//   will regenerate stubs into build/generated/source/proto/.

import com.google.protobuf.gradle.*

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.protobuf)
}

android {
    namespace   = "com.jarvis.client"
    compileSdk  = 35

    defaultConfig {
        applicationId   = "com.jarvis.client"
        minSdk          = 26          // Android 8.0 — AudioRecord + gRPC minimum
        targetSdk       = 35
        versionCode     = 1
        versionName     = "1.0.0"

        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
        }
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    // Proto source sets — points at shared monorepo proto files.
    // Path relative to app/build.gradle.kts: ../../../../proto
    sourceSets {
        getByName("main") {
            proto {
                srcDir("../../../../proto")
            }
        }
    }
}

// ── Protobuf codegen ─────────────────────────────────────────────────────────

protobuf {
    protoc {
        artifact = "com.google.protobuf:protoc:${libs.versions.protobuf.get()}"
    }
    plugins {
        create("grpc") {
            artifact = "io.grpc:protoc-gen-grpc-java:${libs.versions.grpc.get()}"
        }
        create("grpckt") {
            artifact = "io.grpc:protoc-gen-grpc-kotlin:${libs.versions.grpcKotlin.get()}:jdk8@jar"
        }
    }
    generateProtoTasks {
        all().forEach { task ->
            task.plugins {
                create("grpc") { option("lite") }
                create("grpckt") { option("lite") }
            }
            task.builtins {
                create("kotlin") { option("lite") }
            }
        }
    }
}

// ── Dependencies ─────────────────────────────────────────────────────────────

dependencies {
    // AndroidX
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(libs.androidx.lifecycle.viewmodel)
    implementation(libs.androidx.activity.compose)

    // Compose
    implementation(platform(libs.compose.bom))
    implementation(libs.compose.ui)
    implementation(libs.compose.ui.tooling.preview)
    implementation(libs.compose.material3)
    implementation(libs.compose.graphics)
    debugImplementation(libs.compose.ui.tooling)

    // gRPC + Protobuf (lite — smaller than full protobuf for mobile)
    implementation(libs.grpc.okhttp)
    implementation(libs.grpc.protobuf.lite)
    implementation(libs.grpc.stub)
    implementation(libs.grpc.kotlin.stub)
    implementation(libs.protobuf.kotlin.lite)

    // Coroutines
    implementation(libs.coroutines.android)

    // Testing
    testImplementation(libs.junit)
    testImplementation(libs.mockk)
    testImplementation(libs.coroutines.test)
    androidTestImplementation(libs.junit.ktx)
}
