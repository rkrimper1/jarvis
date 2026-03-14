// MainActivity.kt
// Jarvis Android Client

package com.jarvis.client

import android.Manifest
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.compose.runtime.*
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.jarvis.client.ui.screens.HudScreen
import com.jarvis.client.ui.theme.JarvisTheme
import com.jarvis.client.viewmodel.VoiceViewModel
import com.jarvis.client.viewmodel.VoiceViewModelFactory

class MainActivity : ComponentActivity() {

    private val viewModel: VoiceViewModel by viewModels {
        VoiceViewModelFactory(application)
    }

    private val requestPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { granted ->
        viewModel.checkMicPermission()
        if (granted) viewModel.start()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        setContent {
            JarvisTheme {
                val voiceState        by viewModel.voiceState.collectAsStateWithLifecycle()
                val isConnected       by viewModel.isConnected.collectAsStateWithLifecycle()
                val micRms            by viewModel.micRms.collectAsStateWithLifecycle()
                val liveTranscript    by viewModel.liveTranscript.collectAsStateWithLifecycle()
                val transcriptHistory by viewModel.transcriptHistory.collectAsStateWithLifecycle()
                val lastReply         by viewModel.lastReply.collectAsStateWithLifecycle()
                val pendingActions    by viewModel.pendingActions.collectAsStateWithLifecycle()
                val requiresConfirm   by viewModel.requiresConfirmation.collectAsStateWithLifecycle()
                val lastError         by viewModel.lastErrorMessage.collectAsStateWithLifecycle()
                val hasMicPermission  by viewModel.hasMicPermission.collectAsStateWithLifecycle()

                HudScreen(
                    voiceState           = voiceState,
                    isConnected          = isConnected,
                    micRms               = micRms,
                    liveTranscript       = liveTranscript,
                    transcriptHistory    = transcriptHistory,
                    lastReply            = lastReply,
                    pendingActions       = pendingActions,
                    requiresConfirmation = requiresConfirm,
                    lastErrorMessage     = lastError,
                    hasMicPermission     = hasMicPermission,
                    onStart = {
                        if (!hasMicPermission) {
                            requestPermissionLauncher.launch(Manifest.permission.RECORD_AUDIO)
                        } else {
                            viewModel.start()
                        }
                    },
                    onStop         = viewModel::stop,
                    onEndSpeech    = viewModel::endSpeech,
                    onCancel       = viewModel::cancel,
                    onConfirm      = viewModel::confirm,
                    onDismissAction = viewModel::dismissTopAction,
                    onClearHistory = viewModel::clearHistory,
                )
            }
        }
    }

    override fun onStop() {
        super.onStop()
        viewModel.stop()
    }
}
