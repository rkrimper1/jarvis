// JarvisApplication.kt
// Jarvis Android Client

package com.jarvis.client

import android.app.Application
import android.util.Log

class JarvisApplication : Application() {

    override fun onCreate() {
        super.onCreate()
        Log.i("JarvisApplication", "Jarvis Android client initialised")
        // Future: inject Hilt component, initialise crash reporting, etc.
    }
}
