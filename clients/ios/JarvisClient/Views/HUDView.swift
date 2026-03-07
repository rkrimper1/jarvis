// HUDView.swift
// Jarvis iOS Client — clients/ios/JarvisClient/JarvisClient/Views/
//
// Root HUD overlay — composes all panels into a full-screen tactical display.
//
// Layout (portrait):
//   ┌─────────────────────────────┐
//   │  StatusBar  [state] [conn]  │  ← always visible
//   │─────────────────────────────│
//   │                             │
//   │      WaveformView           │  ← centre stage
//   │                             │
//   │─────────────────────────────│
//   │  TranscriptView (scroll)    │  ← conversation history
//   │─────────────────────────────│
//   │  ActionBanner (if pending)  │  ← HUD actions from server
//   │─────────────────────────────│
//   │  ControlBar  [cancel][conf] │  ← user controls
//   └─────────────────────────────┘

import SwiftUI

// MARK: - HUDView

public struct HUDView: View {
    @StateObject private var viewModel: VoiceViewModel
    @State private var appeared = false

    public init(userID: String, configuration: VoiceServiceConfiguration = .development) {
        _viewModel = StateObject(wrappedValue: VoiceViewModel(
            userID: userID,
            grpcConfiguration: configuration
        ))
    }

    public var body: some View {
        ZStack {
            // ── Full-screen background ──────────────────────────────────
            Color.hudBackground.ignoresSafeArea()
            GridBackground()

            // ── Main layout ─────────────────────────────────────────────
            VStack(spacing: 0) {
                StatusBar(viewModel: viewModel)
                    .padding(.horizontal, 16)
                    .padding(.top, 8)

                Divider()
                    .background(Color.hudBorder)
                    .padding(.top, 8)

                // Waveform — central hero element
                WaveformView(viewModel: viewModel)
                    .padding(.vertical, 20)
                    .frame(maxWidth: .infinity)

                Divider()
                    .background(Color.hudBorder)

                // Transcript history
                TranscriptView(viewModel: viewModel)
                    .padding(.horizontal, 16)
                    .padding(.top, 12)

                Spacer(minLength: 8)

                // Pending HUD actions
                if !viewModel.pendingActions.isEmpty {
                    ActionBanner(
                        actions:   viewModel.pendingActions,
                        onDismiss: { viewModel.dismissTopAction() }
                    )
                    .padding(.horizontal, 16)
                    .padding(.bottom, 8)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                }

                // Confirmation prompt
                if viewModel.requiresConfirmation {
                    ConfirmationPrompt(
                        replyText: viewModel.lastReply,
                        onConfirm: { Task { await viewModel.confirm() } },
                        onCancel:  { Task { await viewModel.cancel() } }
                    )
                    .padding(.horizontal, 16)
                    .padding(.bottom, 8)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                }

                Divider()
                    .background(Color.hudBorder)

                ControlBar(viewModel: viewModel)
                    .padding(.horizontal, 16)
                    .padding(.vertical, 12)
            }

            // ── Outer corner brackets on full screen ───────────────────
            CornerBrackets(color: .arcCyan.opacity(0.4), size: 20, lineWidth: 1)
                .padding(12)
                .ignoresSafeArea()

            // ── Error overlay ──────────────────────────────────────────
            if case .error(let msg) = viewModel.voiceState {
                ErrorBanner(message: msg)
                    .padding(.horizontal, 16)
                    .padding(.top, 60)
                    .frame(maxHeight: .infinity, alignment: .top)
                    .transition(.move(edge: .top).combined(with: .opacity))
            }
        }
        .animation(.easeInOut(duration: 0.25), value: viewModel.voiceState.displayLabel)
        .animation(.easeInOut(duration: 0.3), value: viewModel.pendingActions.count)
        .animation(.easeInOut(duration: 0.3), value: viewModel.requiresConfirmation)
        .opacity(appeared ? 1 : 0)
        .scaleEffect(appeared ? 1 : 0.97)
        .task {
            await viewModel.start()
            withAnimation(.easeOut(duration: 0.4)) { appeared = true }
        }
        .onDisappear {
            Task { await viewModel.stop() }
        }
    }
}

// MARK: - StatusBar

private struct StatusBar: View {
    @ObservedObject var viewModel: VoiceViewModel
    @State private var connPulse = false

    var body: some View {
        HStack(alignment: .center, spacing: 0) {
            // Left: Jarvis wordmark
            VStack(alignment: .leading, spacing: 2) {
                Text("J.A.R.V.I.S")
                    .font(.jarvisDisplay(22, weight: .ultraLight))
                    .foregroundStyle(Color.arcCyan)
                    .tracking(6)
                    .glow(.arcCyan, radius: 4)

                Text("JUST A RATHER VERY INTELLIGENT SYSTEM")
                    .font(.jarvisMono(6))
                    .foregroundStyle(Color.hudTertiary)
                    .tracking(1.5)
            }

            Spacer()

            // Right: state badge + connection dot
            VStack(alignment: .trailing, spacing: 6) {
                StateBadge(state: viewModel.voiceState)

                HStack(spacing: 5) {
                    // Connection indicator
                    Circle()
                        .fill(viewModel.isConnected ? Color.stateListening : Color.stateError)
                        .frame(width: 5, height: 5)
                        .scaleEffect(connPulse && viewModel.isConnected ? 1.4 : 1.0)
                        .glow(
                            viewModel.isConnected ? .stateListening : .stateError,
                            radius: 3
                        )
                        .onChange(of: viewModel.isConnected) { _, connected in
                            if connected {
                                withAnimation(.easeInOut(duration: 1).repeatForever()) {
                                    connPulse.toggle()
                                }
                            } else {
                                connPulse = false
                            }
                        }

                    Text(viewModel.isConnected ? "ONLINE" : "OFFLINE")
                        .font(.jarvisMono(8, weight: .medium))
                        .foregroundStyle(viewModel.isConnected
                                         ? Color.stateListening
                                         : Color.stateError)
                        .tracking(1)
                }
            }
        }
    }
}

// MARK: - StateBadge

private struct StateBadge: View {
    let state: VoiceState
    @State private var blinkOpacity: Double = 1.0

    var body: some View {
        HStack(spacing: 6) {
            // Animated state indicator
            if state.isAnimating {
                Circle()
                    .fill(state.hudColor)
                    .frame(width: 6, height: 6)
                    .opacity(blinkOpacity)
                    .glow(state.hudColor, radius: 4)
                    .onAppear {
                        withAnimation(.easeInOut(duration: 0.6).repeatForever()) {
                            blinkOpacity = 0.2
                        }
                    }
                    .onDisappear { blinkOpacity = 1 }
            } else {
                Circle()
                    .stroke(state.hudColor, lineWidth: 1)
                    .frame(width: 6, height: 6)
            }

            Text(state.displayLabel.uppercased())
                .font(.jarvisMono(10, weight: .medium))
                .foregroundStyle(state.hudColor)
                .tracking(1.5)
                .glow(state.hudColor, radius: state.isAnimating ? 3 : 0)
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 5)
        .background(state.hudColor.opacity(0.08))
        .overlay(
            RoundedRectangle(cornerRadius: HUDGeometry.cornerRadius)
                .stroke(state.hudColor.opacity(0.35), lineWidth: 0.5)
        )
        .animation(.easeInOut(duration: 0.2), value: state.displayLabel)
    }
}

// MARK: - ActionBanner

private struct ActionBanner: View {
    let actions: [HUDActionModel]
    let onDismiss: () -> Void

    var body: some View {
        if let top = actions.first {
            HUDPanel(label: "Action Required", accentColor: top.severity.hudColor) {
                HStack(spacing: 12) {
                    // Severity icon
                    SeverityIcon(severity: top.severity)

                    VStack(alignment: .leading, spacing: 4) {
                        Text(top.type.rawValue
                            .replacingOccurrences(of: "TYPE_", with: "")
                            .replacingOccurrences(of: "_", with: " "))
                            .font(.jarvisMono(11, weight: .medium))
                            .foregroundStyle(top.severity.hudColor)
                            .tracking(1)

                        Text(top.payloadJSON)
                            .font(.jarvisMono(9))
                            .foregroundStyle(Color.hudSecondary)
                            .lineLimit(2)
                    }

                    Spacer()

                    Button(action: onDismiss) {
                        Image(systemName: "xmark")
                            .font(.system(size: 10, weight: .light))
                            .foregroundStyle(Color.hudSecondary)
                            .frame(width: 24, height: 24)
                            .background(Color.hudBackground.opacity(0.6))
                            .clipShape(RoundedRectangle(cornerRadius: 2))
                    }
                }
            }
        }
    }
}

// MARK: - SeverityIcon

private struct SeverityIcon: View {
    let severity: HUDSeverity
    @State private var pulse = false

    var body: some View {
        ZStack {
            RoundedRectangle(cornerRadius: 2)
                .fill(severity.hudColor.opacity(0.12))
                .frame(width: 32, height: 32)
                .scaleEffect(pulse ? 1.15 : 1.0)

            RoundedRectangle(cornerRadius: 2)
                .stroke(severity.hudColor.opacity(0.4), lineWidth: 0.5)
                .frame(width: 32, height: 32)

            Image(systemName: severityIcon)
                .font(.system(size: 14, weight: .light))
                .foregroundStyle(severity.hudColor)
        }
        .onAppear {
            if severity == .critical || severity == .emergency {
                withAnimation(.easeInOut(duration: 0.5).repeatForever()) {
                    pulse = true
                }
            }
        }
    }

    private var severityIcon: String {
        switch severity {
        case .info:      return "info"
        case .warning:   return "exclamationmark.triangle"
        case .critical:  return "exclamationmark.2"
        case .emergency: return "exclamationmark.3"
        }
    }
}

// MARK: - ConfirmationPrompt

private struct ConfirmationPrompt: View {
    let replyText: String
    let onConfirm: () -> Void
    let onCancel: () -> Void

    var body: some View {
        HUDPanel(label: "Confirmation Required", accentColor: .stateProcessing) {
            VStack(alignment: .leading, spacing: 12) {
                Text(replyText)
                    .font(.jarvisBody(13))
                    .foregroundStyle(Color.hudSecondary)
                    .lineSpacing(3)

                HStack(spacing: 10) {
                    TacticalButton(
                        label:  "CONFIRM",
                        color:  .stateListening,
                        action: onConfirm
                    )
                    TacticalButton(
                        label:  "CANCEL",
                        color:  .stateError,
                        action: onCancel
                    )
                }
            }
        }
    }
}

// MARK: - ControlBar

private struct ControlBar: View {
    @ObservedObject var viewModel: VoiceViewModel

    var body: some View {
        HStack(spacing: 16) {
            // Session info
            VStack(alignment: .leading, spacing: 3) {
                Text("SESSION ACTIVE")
                    .font(.jarvisMono(8))
                    .foregroundStyle(Color.hudTertiary)
                    .tracking(1)

                Text(viewModel.isConnected ? "STREAM CONNECTED" : "STREAM OFFLINE")
                    .font(.jarvisMono(9, weight: .medium))
                    .foregroundStyle(viewModel.isConnected ? .stateListening : .stateError)
            }

            Spacer()

            // Cancel
            if viewModel.voiceState.isActive {
                TacticalButton(
                    label:  "CANCEL",
                    color:  .stateError,
                    action: { Task { await viewModel.cancel() } }
                )

                // Manual end-of-speech
                if viewModel.voiceState == .listening {
                    TacticalButton(
                        label:  "SEND",
                        color:  .arcCyan,
                        action: { Task { await viewModel.endSpeech() } }
                    )
                }
            }

            // Clear history
            Button(action: { viewModel.clearHistory() }) {
                Image(systemName: "trash")
                    .font(.system(size: 11, weight: .light))
                    .foregroundStyle(Color.hudTertiary)
                    .frame(width: 28, height: 28)
                    .background(Color.hudSurface)
                    .overlay(
                        RoundedRectangle(cornerRadius: 2)
                            .stroke(Color.hudBorder, lineWidth: 0.5)
                    )
            }
        }
    }
}

// MARK: - TacticalButton

private struct TacticalButton: View {
    let label: String
    let color: Color
    let action: () -> Void
    @State private var pressed = false

    var body: some View {
        Button(action: action) {
            Text(label)
                .font(.jarvisMono(10, weight: .medium))
                .foregroundStyle(color)
                .tracking(1.5)
                .padding(.horizontal, 14)
                .padding(.vertical, 7)
                .background(color.opacity(pressed ? 0.15 : 0.06))
                .overlay(
                    RoundedRectangle(cornerRadius: HUDGeometry.cornerRadius)
                        .stroke(color.opacity(0.4), lineWidth: 0.5)
                )
                .clipShape(RoundedRectangle(cornerRadius: HUDGeometry.cornerRadius))
        }
        .buttonStyle(PlainButtonStyle())
        .simultaneousGesture(
            DragGesture(minimumDistance: 0)
                .onChanged { _ in pressed = true }
                .onEnded   { _ in pressed = false }
        )
    }
}

// MARK: - ErrorBanner

private struct ErrorBanner: View {
    let message: String

    var body: some View {
        HStack(spacing: 10) {
            Rectangle()
                .fill(Color.stateError)
                .frame(width: 2)

            VStack(alignment: .leading, spacing: 3) {
                Text("SYSTEM ALERT")
                    .font(.jarvisMono(9, weight: .medium))
                    .foregroundStyle(Color.stateError)
                    .tracking(2)

                Text(message)
                    .font(.jarvisBody(12))
                    .foregroundStyle(Color.hudSecondary)
                    .lineLimit(2)
            }

            Spacer()
        }
        .padding(12)
        .background(Color.stateError.opacity(0.07))
        .overlay(
            RoundedRectangle(cornerRadius: HUDGeometry.cornerRadius)
                .stroke(Color.stateError.opacity(0.3), lineWidth: 0.5)
        )
        .glow(.stateError, radius: 4)
    }
}

// MARK: - GridBackground

private struct GridBackground: View {
    var body: some View {
        Canvas { ctx, size in
            let spacing: CGFloat = 32
            var path = Path()

            // Vertical lines
            var x: CGFloat = 0
            while x <= size.width {
                path.move(to: CGPoint(x: x, y: 0))
                path.addLine(to: CGPoint(x: x, y: size.height))
                x += spacing
            }

            // Horizontal lines
            var y: CGFloat = 0
            while y <= size.height {
                path.move(to: CGPoint(x: 0, y: y))
                path.addLine(to: CGPoint(x: size.width, y: y))
                y += spacing
            }

            ctx.stroke(path, with: .color(Color.hudBorder.opacity(0.5)), lineWidth: 0.3)
        }
        .ignoresSafeArea()
        .allowsHitTesting(false)
    }
}
