# Jarvis Android Client — ProGuard rules

# ── Protobuf ──────────────────────────────────────────────────────────────────
-keep class com.google.protobuf.** { *; }
-keepclassmembers class * extends com.google.protobuf.GeneratedMessageLite {
    <fields>;
}

# ── gRPC ──────────────────────────────────────────────────────────────────────
-keep class io.grpc.** { *; }
-keep class io.grpc.okhttp.** { *; }

# ── Generated proto stubs (jarvis package) ─────────────────────────────────────
-keep class jarvis.** { *; }

# ── Kotlin coroutines ─────────────────────────────────────────────────────────
-keepnames class kotlinx.coroutines.internal.MainDispatcherFactory {}
-keepnames class kotlinx.coroutines.CoroutineExceptionHandler {}
-keepclassmembernames class kotlinx.** {
    volatile <fields>;
}
