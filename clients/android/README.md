# Jarvis Android Client

Kotlin/Jetpack Compose voice client for the Jarvis backend.
Structurally parallel to the iOS client — same gRPC protocol, same
state machine, mirrored public API across AudioCaptureEngine, WakeWordDetector,
and GRPCVoiceService.

## Location in monorepo

```
jarvis/
├── clients/
│   ├── ios/                        ← Swift/SwiftUI client
│   └── android/
│       ├── settings.gradle.kts
│       ├── build.gradle.kts
│       ├── gradle/
│       │   └── libs.versions.toml  ← version catalog
│       └── app/
│           ├── build.gradle.kts    ← gRPC + protobuf codegen
│           ├── proguard-rules.pro
│           └── src/
│               ├── main/
│               │   ├── AndroidManifest.xml
│               │   ├── kotlin/com/jarvis/client/
│               │   │   ├── JarvisApplication.kt
│               │   │   ├── MainActivity.kt
│               │   │   ├── audio/
│               │   │   │   └── AudioCaptureEngine.kt
│               │   │   ├── grpc/
│               │   │   │   └── GRPCVoiceService.kt
│               │   │   ├── ui/
│               │   │   │   ├── components/WaveformView.kt
│               │   │   │   ├── screens/HudScreen.kt
│               │   │   │   └── theme/Theme.kt
│               │   │   ├── viewmodel/
│               │   │   │   ├── VoiceViewModel.kt
│               │   │   │   └── VoiceViewModelFactory.kt
│               │   │   └── wakeword/
│               │   │       └── WakeWordDetector.kt
│               │   └── res/values/strings.xml
│               └── test/kotlin/com/jarvis/client/
│                   ├── AudioCaptureEngineTest.kt
│                   ├── WakeWordDetectorTest.kt
│                   ├── GRPCVoiceServiceTest.kt
│                   └── VoiceViewModelTest.kt
├── gen/
│   └── go/                         ← generated Go stubs
└── proto/
    └── voice/voice.proto           ← source of truth
```

## Prerequisites

- Android Studio Hedgehog (2023.1) or later
- Android SDK 35 (compile) / SDK 26 min (Android 8.0)
- `buf` CLI for proto codegen (`brew install bufbuild/buf/buf`)
- A running Jarvis backend (`docker compose up` from repo root)

## Getting started

```bash
# 1. Generate Android proto stubs
#    (run from the monorepo root — output lands in gen/android/)
make proto-android

# 2. Open in Android Studio
open clients/android

# 3. Run on device / emulator
./gradlew installDebug
```

## Proto → Kotlin codegen

The `protobuf` Gradle plugin reads `proto/voice/voice.proto` and
`proto/common/common.proto` relative to the `app/` module and generates:

- Kotlin lite proto classes (via the built-in `kotlin` builtin)
- Java gRPC async stub (`VoiceServiceGrpc`) via `protoc-gen-grpc-java`
- Kotlin coroutine stub (`VoiceServiceGrpcKt`) via `protoc-gen-grpc-kotlin`

Output lands in `app/build/generated/source/proto/` and is automatically
added to the compile classpath. Run `./gradlew generateDebugProto` to
regenerate without a full build.

## Configuration

Edit `VoiceServiceConfiguration` in `GRPCVoiceService.kt`:

| Preset        | Host                         | Port  | TLS  |
|---------------|------------------------------|-------|------|
| `Development` | `localhost`                  | 50059 | No   |
| `Production`  | `voice.jarvis.yourdomain.com`| 443   | Yes  |

Pass a custom config to `VoiceViewModelFactory`:

```kotlin
val factory = VoiceViewModelFactory(
    application = application,
    grpcConfig  = VoiceServiceConfiguration(
        host    = "192.168.1.100",   // your local machine
        port    = 50059,
        useTls  = false,
    ),
)
val viewModel: VoiceViewModel by viewModels { factory }
```

## Architecture

```
MainActivity
    └── VoiceViewModel
            ├── AudioCaptureEngine   — AudioRecord → 16 kHz PCM-16LE frames
            ├── WakeWordDetector     — SpeechRecognizer continuous listen
            ├── GRPCVoiceService     — bidirectional VoiceService.Converse stream
            └── TtsPlayer            — AudioTrack PCM playback of TTS replies
```

### State machine

```
IDLE ──[wake word]──▶ LISTENING ──[end of speech]──▶ PROCESSING
  ▲                                                        │
  └────── IDLE ◀── SPEAKING ◀────────────────────────────┘
```

Server `StatusEvent.State` drives `VoiceState` transitions in `VoiceViewModel`.

### Wake word backends

| Backend              | Default | Notes                                      |
|----------------------|---------|--------------------------------------------|
| `SPEECH_RECOGNIZER`  | ✅ Yes  | Android SpeechRecognizer, zero extra deps  |
| `ON_DEVICE_KEYWORD`  | No      | TFLite stub — wire a model in Phase 2      |

### Audio pipeline

```
AudioRecord (VOICE_RECOGNITION source)
    → 16 kHz mono PCM-16LE
    → 20 ms frames (640 bytes / 320 samples)
    → SharedFlow<AudioFrame>
    → WakeWordDetector.feed()
    → GRPCVoiceService.sendAudioChunk()   (when isStreaming)
```

Audio source `VOICE_RECOGNITION` applies hardware AGC and noise suppression —
equivalent to iOS `AVAudioSession.mode = .voiceChat`.

## Running tests

```bash
# Unit tests (JVM — no device needed)
./gradlew test

# Instrumented tests (requires connected device or emulator)
./gradlew connectedAndroidTest
```

## Enabling Redis session persistence

Set `SESSION_PROVIDER=redis` in the voice-service docker-compose environment.
The Android client reconnects with the same `sessionId` so the Redis-backed
session store stitches streams across reconnects — no client-side change needed.

## Activating Cloud STT / TTS

Set `STT_PROVIDER=cloud_speech` and `TTS_PROVIDER=cloud_tts` with the
appropriate `GCP_PROJECT` and `GOOGLE_APPLICATION_CREDENTIALS` in
`docker/docker-compose.yml`. The Android client is backend-agnostic.
