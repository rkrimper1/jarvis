// swift-tools-version: 5.9
// Package.swift — Jarvis iOS Client
//
// Manages gRPC + Protobuf dependencies for the JarvisClient Xcode project.
// The generated Swift stubs in jarvis/gen/swift/ are added as a local source
// target so Xcode picks up any proto changes after `make proto`.

import PackageDescription

let package = Package(
    name: "JarvisClient",
    platforms: [
        .iOS(.v17)
    ],
    products: [
        .library(name: "JarvisClient", targets: ["JarvisClient"]),
    ],
    dependencies: [
        // gRPC Swift runtime + NIO transport
        .package(
            url: "https://github.com/grpc/grpc-swift",
            from: "1.23.0"
        ),
        // Swift Protobuf runtime
        .package(
            url: "https://github.com/apple/swift-protobuf",
            from: "1.26.0"
        ),
    ],
    targets: [
        // ── Generated proto stubs ──────────────────────────────────────
        // Points at the shared gen/swift/ output from `make proto`.
        // Path is relative to this Package.swift location.
        .target(
            name: "JarvisProto",
            dependencies: [
                .product(name: "GRPC",           package: "grpc-swift"),
                .product(name: "SwiftProtobuf",  package: "swift-protobuf"),
            ],
            // Symlinked or copied by `make proto-ios` into this path.
            path: "../../../../gen/swift",
            swiftSettings: [
                .unsafeFlags(["-suppress-warnings"])   // generated code may have warnings
            ]
        ),

        // ── Application target ─────────────────────────────────────────
        .target(
            name: "JarvisClient",
            dependencies: [
                "JarvisProto",
                .product(name: "GRPC",           package: "grpc-swift"),
                .product(name: "SwiftProtobuf",  package: "swift-protobuf"),
            ],
            path: "JarvisClient"
        ),

        // ── Tests ──────────────────────────────────────────────────────
        .testTarget(
            name: "JarvisClientTests",
            dependencies: ["JarvisClient"],
            path: "JarvisClientTests"
        ),
    ]
)
