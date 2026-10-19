import AppKit
import SwiftUI

/// Over the panes while the session's host is away: the terminals dim, and
/// a small card says what's happening and why. Typing goes nowhere then;
/// this is what says so. Gone the moment the host is back.
struct HostAway: View {
    @ObservedObject var model: AppModel
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        let s = model.style
        ZStack {
            Rectangle().fill(s.windowBottom.swiftUI.opacity(0.55)).accessibilityHidden(true)
            HStack(alignment: .top, spacing: 10) {
                // Refused won't change by waiting: the error colour. Trying
                // again is just a state: the accent, pulsing while it tries.
                Group {
                    if model.statusRefused {
                        Image(systemName: "hand.raised.fill").foregroundStyle(s.blocked.swiftUI)
                    } else {
                        Image(systemName: "arrow.triangle.2.circlepath").foregroundStyle(s.accent.swiftUI)
                            .symbolEffect(.pulse, options: .repeating, isActive: !reduceMotion)
                    }
                }
                .font(.system(size: 13, weight: .semibold))
                .padding(.top, 1)
                VStack(alignment: .leading, spacing: 3) {
                    Text(model.statusSentence).font(.system(size: 13, weight: .semibold)).foregroundStyle(s.text.swiftUI)
                    if let d = model.statusDetail {
                        Text(d).font(.system(size: 11.5)).foregroundStyle(s.secondary.swiftUI)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
            }
            .padding(.horizontal, 16).padding(.vertical, 12)
            .frame(maxWidth: 360, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: 12, style: .continuous).fill(s.card.swiftUI))
            .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(s.border.swiftUI, lineWidth: 1))
            .shadow(color: .black.opacity(0.25), radius: 18, y: 6)
        }
    }
}

/// Its host view: hidden, and so letting every event through, while the
/// host is connected. A hidden NSHostingView still answers hit tests: it
/// took the release of every click in the panes (programs saw a press and
/// never a release).
final class HostAwayHost: NSHostingView<HostAway> {
    override func hitTest(_ point: NSPoint) -> NSView? { isHidden ? nil : super.hitTest(point) }
}
