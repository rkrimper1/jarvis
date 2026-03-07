// TranscriptView.swift
// Jarvis iOS Client — clients/ios/JarvisClient/JarvisClient/Views/
//
// Scrolling conversation transcript panel.
// Shows the rolling history of user utterances (STT) and Jarvis replies (NLP)
// with distinct visual treatment for each source, plus a live partial-transcript
// line that updates as SFSpeech emits interim results.

import SwiftUI

// MARK: - TranscriptView

public struct TranscriptView: View {
    @ObservedObject var viewModel: VoiceViewModel

    public var body: some View {
        HUDPanel(label: "Transcript", accentColor: .arcCyan) {
            VStack(alignment: .leading, spacing: 0) {
                // Scrolling history
                ScrollViewReader { proxy in
                    ScrollView(.vertical, showsIndicators: false) {
                        LazyVStack(alignment: .leading, spacing: 2) {
                            ForEach(viewModel.transcriptHistory) { line in
                                TranscriptLineView(line: line)
                                    .id(line.id)
                                    .transition(
                                        .asymmetric(
                                            insertion: .move(edge: .bottom).combined(with: .opacity),
                                            removal:   .opacity
                                        )
                                    )
                            }

                            // Live partial transcript
                            if !viewModel.liveTranscript.isEmpty {
                                PartialTranscriptLine(text: viewModel.liveTranscript)
                                    .id("live")
                            }
                        }
                        .padding(.bottom, 4)
                    }
                    .frame(maxHeight: 220)
                    .onChange(of: viewModel.transcriptHistory.count) { _, _ in
                        withAnimation(.easeOut(duration: 0.2)) {
                            proxy.scrollTo(viewModel.transcriptHistory.last?.id ?? "live", anchor: .bottom)
                        }
                    }
                    .onChange(of: viewModel.liveTranscript) { _, _ in
                        withAnimation {
                            proxy.scrollTo("live", anchor: .bottom)
                        }
                    }
                }

                // Confidence + intent footer
                if !viewModel.lastIntent.isEmpty {
                    IntentBadge(intent: viewModel.lastIntent)
                        .padding(.top, 8)
                }
            }
        }
        .overlay(ScanLineOverlay())
    }
}

// MARK: - TranscriptLineView

private struct TranscriptLineView: View {
    let line: TranscriptLine

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            // Source indicator bar
            Rectangle()
                .fill(sourceColor)
                .frame(width: 1.5)
                .padding(.vertical, 2)

            VStack(alignment: .leading, spacing: 3) {
                // Source label + timestamp
                HStack(spacing: 6) {
                    Text(sourceLabel.uppercased())
                        .font(.jarvisMono(8, weight: .medium))
                        .foregroundStyle(sourceColor)
                        .tracking(1.5)

                    Text(line.timestamp.formatted(.dateTime.hour().minute().second()))
                        .font(.jarvisMono(8))
                        .foregroundStyle(Color.hudTertiary)

                    if line.source == .user && line.isFinal {
                        ConfidencePip(confidence: line.confidence)
                    }
                }

                // Line text
                Text(line.text)
                    .font(line.source == .jarvis
                          ? .jarvisBody(13)
                          : .jarvisMono(12))
                    .foregroundStyle(textColor)
                    .fixedSize(horizontal: false, vertical: true)
                    .lineSpacing(2)
            }
        }
        .padding(.vertical, 6)
        .padding(.trailing, 4)
    }

    private var sourceColor: Color {
        line.source == .user ? .stateListening : .arcCyan
    }

    private var textColor: Color {
        line.source == .user ? .hudPrimary : Color(hex: "#B0D8F0")
    }

    private var sourceLabel: String {
        line.source == .user ? "YOU" : "JARVIS"
    }
}

// MARK: - PartialTranscriptLine

private struct PartialTranscriptLine: View {
    let text: String
    @State private var cursorVisible = true

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Rectangle()
                .fill(Color.stateListening.opacity(0.5))
                .frame(width: 1.5)
                .padding(.vertical, 2)

            HStack(spacing: 0) {
                Text(text)
                    .font(.jarvisMono(12))
                    .foregroundStyle(Color.hudSecondary)

                // Blinking cursor
                Rectangle()
                    .fill(Color.stateListening)
                    .frame(width: 6, height: 12)
                    .opacity(cursorVisible ? 1 : 0)
                    .padding(.leading, 2)
            }
        }
        .padding(.vertical, 6)
        .onAppear {
            withAnimation(.easeInOut(duration: 0.5).repeatForever()) {
                cursorVisible.toggle()
            }
        }
    }
}

// MARK: - ConfidencePip

private struct ConfidencePip: View {
    let confidence: Float

    var body: some View {
        HStack(spacing: 2) {
            ForEach(0..<5, id: \.self) { i in
                Rectangle()
                    .fill(pipColor(i))
                    .frame(width: 2, height: 6)
            }
        }
    }

    private func pipColor(_ index: Int) -> Color {
        let threshold = Float(index + 1) / 5.0
        return confidence >= threshold
            ? confidenceColor.opacity(0.8 + Double(confidence) * 0.2)
            : Color.hudTertiary
    }

    private var confidenceColor: Color {
        if confidence > 0.85 { return .stateListening }
        if confidence > 0.60 { return .stateProcessing }
        return .stateError
    }
}

// MARK: - IntentBadge

private struct IntentBadge: View {
    let intent: String

    var body: some View {
        HStack(spacing: 6) {
            Rectangle()
                .fill(Color.stateProcessing)
                .frame(width: 2, height: 8)

            Text("INTENT")
                .font(.jarvisMono(8, weight: .medium))
                .foregroundStyle(Color.stateProcessing)
                .tracking(1.5)

            Text(intent.replacingOccurrences(of: "INTENT_", with: "").uppercased())
                .font(.jarvisMono(9))
                .foregroundStyle(Color.hudSecondary)
                .lineLimit(1)

            Spacer()
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 5)
        .background(Color.stateProcessing.opacity(0.06))
        .overlay(
            RoundedRectangle(cornerRadius: HUDGeometry.cornerRadius)
                .stroke(Color.stateProcessing.opacity(0.2), lineWidth: 0.5)
        )
    }
}
