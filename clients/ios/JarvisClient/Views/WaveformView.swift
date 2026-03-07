// WaveformView.swift
// Jarvis iOS Client — clients/ios/JarvisClient/JarvisClient/Views/
//
// Real-time audio waveform visualiser.
// Renders a circular arc-reactor ring + radial bar graph driven by
// AudioCaptureEngine's live RMS values from VoiceViewModel.micRMS.
//
// Two layers:
//   1. ArcReactorRing  — pulsing outer ring scaled to RMS energy
//   2. BarWaveform     — rolling history of 64 RMS samples as radial bars

import SwiftUI

// MARK: - WaveformView

public struct WaveformView: View {
    @ObservedObject var viewModel: VoiceViewModel

    // Rolling RMS history — 64 samples, newest at end.
    @State private var rmsHistory: [Float] = Array(repeating: 0, count: 64)
    @State private var pulseScale: CGFloat = 1.0
    @State private var rotationAngle: Double = 0

    private let barCount = 64
    private let size: CGFloat = 160

    public var body: some View {
        ZStack {
            // Ambient glow backdrop
            Circle()
                .fill(viewModel.voiceState.hudColor.opacity(0.04))
                .frame(width: size + 40, height: size + 40)

            // Outer rotating ring
            RotatingRing(color: viewModel.voiceState.hudColor, size: size + 24)
                .rotationEffect(.degrees(rotationAngle))

            // Radial bar waveform
            RadialBarWaveform(
                samples:  rmsHistory,
                barCount: barCount,
                size:     size,
                color:    viewModel.voiceState.hudColor
            )

            // Arc reactor core
            ArcReactorCore(
                rms:   viewModel.micRMS,
                state: viewModel.voiceState,
                size:  size * 0.38
            )

            // Corner brackets overlay
            CornerBrackets(
                color:     viewModel.voiceState.hudColor,
                size:      14,
                lineWidth: 1
            )
            .frame(width: size + 4, height: size + 4)
        }
        .frame(width: size + 44, height: size + 44)
        .onChange(of: viewModel.micRMS) { _, newRMS in
            appendRMS(newRMS)
        }
        .onAppear {
            startRotation()
        }
    }

    // MARK: - Private

    private func appendRMS(_ rms: Float) {
        rmsHistory.removeFirst()
        rmsHistory.append(rms)
    }

    private func startRotation() {
        withAnimation(.linear(duration: 12).repeatForever(autoreverses: false)) {
            rotationAngle = 360
        }
    }
}

// MARK: - RadialBarWaveform

private struct RadialBarWaveform: View {
    let samples: [Float]
    let barCount: Int
    let size: CGFloat
    let color: Color

    private let innerRadius: CGFloat
    private let maxBarHeight: CGFloat

    init(samples: [Float], barCount: Int, size: CGFloat, color: Color) {
        self.samples = samples
        self.barCount = barCount
        self.size = size
        self.color = color
        self.innerRadius = size * 0.42
        self.maxBarHeight = size * 0.12
    }

    var body: some View {
        Canvas { ctx, canvasSize in
            let center = CGPoint(x: canvasSize.width / 2, y: canvasSize.height / 2)
            let angleStep = (2 * Double.pi) / Double(barCount)

            for i in 0 ..< barCount {
                let sample = CGFloat(samples[i])
                let angle  = Double(i) * angleStep - Double.pi / 2
                let barH   = maxBarHeight * sample + 2  // min 2 pt so bars always visible

                let inner = CGPoint(
                    x: center.x + innerRadius * CGFloat(cos(angle)),
                    y: center.y + innerRadius * CGFloat(sin(angle))
                )
                let outer = CGPoint(
                    x: center.x + (innerRadius + barH) * CGFloat(cos(angle)),
                    y: center.y + (innerRadius + barH) * CGFloat(sin(angle))
                )

                var path = Path()
                path.move(to: inner)
                path.addLine(to: outer)

                let alpha = 0.3 + Double(sample) * 0.7
                ctx.stroke(
                    path,
                    with: .color(color.opacity(alpha)),
                    style: StrokeStyle(lineWidth: 1.5, lineCap: .round)
                )
            }
        }
        .frame(width: size, height: size)
        .animation(.easeOut(duration: 0.05), value: samples.last)
    }
}

// MARK: - RotatingRing

private struct RotatingRing: View {
    let color: Color
    let size: CGFloat

    var body: some View {
        ZStack {
            // Dashed base ring
            Circle()
                .stroke(
                    color.opacity(0.15),
                    style: StrokeStyle(lineWidth: 0.5, dash: [4, 6])
                )
                .frame(width: size, height: size)

            // Moving arc segment
            Circle()
                .trim(from: 0, to: 0.25)
                .stroke(
                    LinearGradient(
                        colors: [color.opacity(0), color, color.opacity(0)],
                        startPoint: .leading,
                        endPoint: .trailing
                    ),
                    style: StrokeStyle(lineWidth: 1, lineCap: .round)
                )
                .frame(width: size, height: size)
                .glow(color, radius: 4)
        }
    }
}

// MARK: - ArcReactorCore

private struct ArcReactorCore: View {
    let rms: Float
    let state: VoiceState
    let size: CGFloat

    @State private var innerPulse: CGFloat = 1.0

    var body: some View {
        ZStack {
            // Outer glow ring
            Circle()
                .stroke(state.hudColor.opacity(0.4), lineWidth: 1)
                .frame(width: size, height: size)
                .scaleEffect(innerPulse)
                .glow(state.hudColor, radius: 6)

            // Mid ring
            Circle()
                .stroke(state.hudColor.opacity(0.7), lineWidth: 0.5)
                .frame(width: size * 0.7, height: size * 0.7)

            // Hexagon core
            HexagonShape()
                .fill(
                    RadialGradient(
                        colors: [
                            state.hudColor.opacity(0.3),
                            state.hudColor.opacity(0.05)
                        ],
                        center: .center,
                        startRadius: 0,
                        endRadius: size * 0.25
                    )
                )
                .frame(width: size * 0.55, height: size * 0.55)
                .overlay(
                    HexagonShape()
                        .stroke(state.hudColor.opacity(0.6), lineWidth: 0.5)
                )
                .glow(state.hudColor, radius: 4)

            // Centre dot
            Circle()
                .fill(state.hudColor)
                .frame(width: 4, height: 4)
                .glow(state.hudColor, radius: 6)
        }
        .onChange(of: rms) { _, newRMS in
            let target = 1.0 + CGFloat(newRMS) * 0.12
            withAnimation(.easeOut(duration: 0.08)) {
                innerPulse = target
            }
            withAnimation(.easeIn(duration: 0.12).delay(0.08)) {
                innerPulse = 1.0
            }
        }
        .onChange(of: state) { _, _ in
            if state.isAnimating {
                withAnimation(.easeInOut(duration: 0.9).repeatForever(autoreverses: true)) {
                    innerPulse = 1.06
                }
            } else {
                withAnimation(.easeOut(duration: 0.3)) {
                    innerPulse = 1.0
                }
            }
        }
    }
}

// MARK: - HexagonShape

private struct HexagonShape: Shape {
    func path(in rect: CGRect) -> Path {
        var path = Path()
        let center = CGPoint(x: rect.midX, y: rect.midY)
        let r = min(rect.width, rect.height) / 2
        for i in 0 ..< 6 {
            let angle = Double(i) * (Double.pi / 3) - Double.pi / 6
            let pt = CGPoint(
                x: center.x + r * CGFloat(cos(angle)),
                y: center.y + r * CGFloat(sin(angle))
            )
            if i == 0 { path.move(to: pt) } else { path.addLine(to: pt) }
        }
        path.closeSubpath()
        return path
    }
}
