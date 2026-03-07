// JarvisApp.swift
// Jarvis iOS Client — clients/ios/JarvisClient/JarvisClient/App/

import SwiftUI

@main
struct JarvisApp: App {

    var body: some Scene {
        WindowGroup {
            HUDView(
                userID:        "tony",
                configuration: .development    // swap to .production for release
            )
            .preferredColorScheme(.dark)
            .persistentSystemOverlays(.hidden) // full-screen tactical display
            .statusBarHidden(true)
        }
    }
}
