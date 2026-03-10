// VoiceViewModelFactory.kt
// Jarvis Android Client — viewmodel/
//
// AndroidViewModel factory. Allows MainActivity to pass Application context
// and optional configuration overrides without breaking the ViewModel lifecycle.

package com.jarvis.client.viewmodel

import android.app.Application
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import com.jarvis.client.grpc.VoiceServiceConfiguration
import com.jarvis.client.wakeword.WakeWordConfiguration

class VoiceViewModelFactory(
    private val application:  Application,
    private val grpcConfig:   VoiceServiceConfiguration = VoiceServiceConfiguration.Development,
    private val wakeConfig:   WakeWordConfiguration     = WakeWordConfiguration.Default,
) : ViewModelProvider.Factory {

    @Suppress("UNCHECKED_CAST")
    override fun <T : ViewModel> create(modelClass: Class<T>): T {
        if (modelClass.isAssignableFrom(VoiceViewModel::class.java)) {
            return VoiceViewModel(application, grpcConfig, wakeConfig) as T
        }
        throw IllegalArgumentException("Unknown ViewModel class: ${modelClass.name}")
    }
}
