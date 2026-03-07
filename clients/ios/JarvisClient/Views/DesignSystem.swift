// DesignSystem.swift
// Jarvis iOS Client — clients/ios/JarvisClient/JarvisClient/Views/
//
// All design tokens for the Jarvis HUD.
// Aesthetic direction: Iron Man MARK L tactical overlay —
// deep-space black, arc-reactor cyan, sharp geometry, no softness.

import SwiftUI

// MARK: - Color Palette

extension Color {
    // Backgrounds
    static let hudBackground   = Color(hex: "#050A0F")   // near-black with blue tint
    static let hudSurface      = Color(hex: "#0A1520")   // elevated surface
    static let hudBorder       = Color(hex: "#0D2137")   // subtle grid lines

    // Arc reactor cyan — primary brand
    static let arcCyan         = Color(hex: "#00D4FF")
    static let arcCyanDim      = Color(hex: "#0088AA")
    static let arcCyanGlow     = Color(hex: "#00D4FF").opacity(0.15)

    // State colours
    static let stateListening  = Color(hex: "#00FF9F")   // green — active mic
    static let stateProcessing = Color(hex: "#FFB800")   // amber — thinking
    static let stateSpeaking   = Color(hex: "#00D4FF")   // cyan — output
    static let stateError      = Color(hex: "#FF3B30")   // red — alert

    // Severity
    static let severityInfo      = Color(hex: "#00D4FF")
    static let severityWarning   = Color(hex: "#FFB800")
    static let severityCritical  = Color(hex: "#FF6B00")
    static let severityEmergency = Color(hex: "#FF3B30")

    // Text
    static let hudPrimary    = Color.white
    static let hudSecondary  = Color(hex: "#6B9EBF")
    static let hudTertiary   = Color(hex: "#2E4D63")
}

extension Color {
    init(hex: String) {
        let h = hex.trimmingCharacters(in: CharacterSet.alphanumerics.inverted)
        var int: UInt64 = 0
        Scanner(string: h).scanHexInt64(&int)
        let r = Double((int >> 16) & 0xFF) / 255
        let g = Double((int >>  8) & 0xFF) / 255
        let b = Double( int        & 0xFF) / 255
        self.init(red: r, green: g, blue: b)
    }
}

// MARK: - Typography

extension Font {
    /// Jarvis display font — large labels, state text.
    static func jarvisDisplay(_ size: CGFloat, weight: Font.Weight = .thin) -> Font {
        .system(size: size, weight: weight, design: .monospaced)
    }

    /// Tactical data font — numbers, coordinates, telemetry.
    static func jarvisMono(_ size: CGFloat, weight: Font.Weight = .regular) -> Font {
        .system(size: size, weight: weight, design: .monospaced)
    }

    /// Body copy — transcript lines, replies.
    static func jarvisBody(_ size: CGFloat) -> Font {
        .system(size: size, weight: .light, design: .default)
    }
}

// MARK: - Geometry

struct HUDGeometry {
    static let cornerRadius: CGFloat     = 2      // sharp, not rounded
    static let borderWidth: CGFloat      = 0.5
    static let gridSpacing: CGFloat      = 1
    static let glowRadius: CGFloat       = 8
    static let panelPadding: CGFloat     = 16
    static let sectionSpacing: CGFloat   = 12
}

// MARK: - Glow Modifier

struct GlowModifier: ViewModifier {
    let color: Color
    let radius: CGFloat

    func body(content: Content) -> some View {
        content
            .shadow(color: color.opacity(0.8), radius: radius * 0.5)
            .shadow(color: color.opacity(0.4), radius: radius)
            .shadow(color: color.opacity(0.2), radius: radius * 2)
    }
}

extension View {
    func glow(_ color: Color = .arcCyan, radius: CGFloat = 8) -> some View {
        modifier(GlowModifier(color: color, radius: radius))
    }
}

// MARK: - HUD Panel

/// Standard bordered panel used throughout the HUD.
struct HUDPanel<Content: View>: View {
    var label: String? = nil
    var accentColor: Color = .arcCyan
    @ViewBuilder let content: () -> Content

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let label {
                HStack(spacing: 6) {
                    Rectangle()
                        .fill(accentColor)
                        .frame(width: 2, height: 10)
                    Text(label.uppercased())
                        .font(.jarvisMono(9, weight: .medium))
                        .foregroundStyle(accentColor)
                        .tracking(2)
                    Spacer()
                    // Corner tick marks
                    HUDTickMark()
                }
                .padding(.horizontal, HUDGeometry.panelPadding)
                .padding(.top, 10)
                .padding(.bottom, 8)

                Rectangle()
                    .fill(accentColor.opacity(0.2))
                    .frame(height: HUDGeometry.borderWidth)
            }

            content()
                .padding(HUDGeometry.panelPadding)
        }
        .background(Color.hudSurface)
        .overlay(
            RoundedRectangle(cornerRadius: HUDGeometry.cornerRadius)
                .stroke(Color.hudBorder, lineWidth: HUDGeometry.borderWidth)
        )
        .clipShape(RoundedRectangle(cornerRadius: HUDGeometry.cornerRadius))
    }
}

// MARK: - Corner Brackets

/// Four-corner bracket decoration — classic tactical HUD motif.
struct CornerBrackets: View {
    var color: Color = .arcCyan
    var size: CGFloat = 12
    var lineWidth: CGFloat = 1.5

    var body: some View {
        GeometryReader { geo in
            ZStack {
                // Top-left
                bracket(rotation: 0)
                    .position(x: size / 2, y: size / 2)
                // Top-right
                bracket(rotation: 90)
                    .position(x: geo.size.width - size / 2, y: size / 2)
                // Bottom-right
                bracket(rotation: 180)
                    .position(x: geo.size.width - size / 2, y: geo.size.height - size / 2)
                // Bottom-left
                bracket(rotation: 270)
                    .position(x: size / 2, y: geo.size.height - size / 2)
            }
        }
    }

    private func bracket(rotation: Double) -> some View {
        Path { p in
            p.move(to: CGPoint(x: size, y: 0))
            p.addLine(to: CGPoint(x: 0, y: 0))
            p.addLine(to: CGPoint(x: 0, y: size))
        }
        .stroke(color, lineWidth: lineWidth)
        .frame(width: size, height: size)
        .rotationEffect(.degrees(rotation))
    }
}

// MARK: - HUD Tick Mark

struct HUDTickMark: View {
    var body: some View {
        HStack(spacing: 3) {
            ForEach(0..<3, id: \.self) { _ in
                Rectangle()
                    .fill(Color.hudTertiary)
                    .frame(width: 1, height: 6)
            }
        }
    }
}

// MARK: - Scan Line Overlay

/// Subtle moving scan line — adds life to static panels.
struct ScanLineOverlay: View {
    @State private var offset: CGFloat = -200

    var body: some View {
        GeometryReader { geo in
            Rectangle()
                .fill(
                    LinearGradient(
                        colors: [.clear, Color.arcCyan.opacity(0.03), .clear],
                        startPoint: .top,
                        endPoint: .bottom
                    )
                )
                .frame(height: 40)
                .offset(y: offset)
                .onAppear {
                    withAnimation(.linear(duration: 4).repeatForever(autoreverses: false)) {
                        offset = geo.size.height + 40
                    }
                }
        }
        .clipped()
        .allowsHitTesting(false)
    }
}

// MARK: - State Color Helper

extension VoiceState {
    var hudColor: Color {
        switch self {
        case .idle:        return .arcCyanDim
        case .listening:   return .stateListening
        case .processing:  return .stateProcessing
        case .speaking:    return .stateSpeaking
        case .error:       return .stateError
        }
    }

    var isAnimating: Bool {
        switch self {
        case .listening, .processing, .speaking: return true
        default: return false
        }
    }
}

extension HUDSeverity {
    var hudColor: Color {
        switch self {
        case .info:      return .severityInfo
        case .warning:   return .severityWarning
        case .critical:  return .severityCritical
        case .emergency: return .severityEmergency
        }
    }
}
