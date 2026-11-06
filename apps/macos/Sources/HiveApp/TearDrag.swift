import AppKit
import HiveCore
import HiveRender
import SwiftUI

/// A tab being torn into its own window: a card of it follows the pointer, shaped like the
/// window it becomes (its title bar on top, its panes under it), and letting go opens the
/// window where the card is.
@MainActor
final class TearDrag {
    static let shared = TearDrag()
    weak var model: AppModel?
    var renderer: Renderer?
    /// The main window's pane area, in points: the picture's proportions and the new window's size.
    var paneArea: () -> CGSize = { CGSize(width: 900, height: 560) }

    private var panel: NSPanel?
    private var id: String?
    /// Where the pointer holds the card, from its top left.
    private var grip = CGPoint(x: 106, y: 15)
    static let cardWidth: CGFloat = 320

    func begin(_ id: String) {
        guard let model else { return }
        drop(commit: false)
        self.id = id
        let area = paneArea()
        let aspect = max(0.4, min(1.2, area.height / max(area.width, 1)))
        let pictureHeight = (TearDrag.cardWidth - 16) * aspect
        let size = CGSize(width: TearDrag.cardWidth + 40, height: pictureHeight + 32 + 8 + 40)  // room for the shadow
        let p = NSPanel(contentRect: NSRect(origin: .zero, size: size), styleMask: [.borderless, .nonactivatingPanel],
                        backing: .buffered, defer: false)
        p.isOpaque = false
        p.backgroundColor = .clear
        p.hasShadow = false
        p.level = .floating
        p.ignoresMouseEvents = true
        p.isReleasedWhenClosed = false
        p.collectionBehavior = [.canJoinAllSpaces, .transient, .ignoresCycle]
        let host = NSHostingView(rootView: TearCard(model: model, id: id, picture: picture(of: id, height: pictureHeight),
                                                    pictureHeight: pictureHeight, pops: model.motion != nil))
        host.frame = NSRect(origin: .zero, size: size)
        p.contentView = host
        grip = CGPoint(x: 20 + min(120, TearDrag.cardWidth / 3), y: 20 + 16)
        panel = p
        move()
        p.orderFront(nil)
    }

    func move() {
        guard let p = panel else { return }
        let m = NSEvent.mouseLocation
        p.setFrameOrigin(CGPoint(x: m.x - grip.x, y: m.y - (p.frame.height - grip.y)))
    }

    /// Let go: commit opens the window at the pointer; else the card just goes.
    func drop(commit: Bool) {
        guard let p = panel else { return }
        let id = self.id
        panel = nil
        self.id = nil
        if commit, let id, let model {
            let m = NSEvent.mouseLocation
            let screen = NSScreen.screens.first { $0.frame.contains(m) }?.visibleFrame ?? NSScreen.main?.visibleFrame ?? .zero
            let area = paneArea()
            let size = CGSize(width: max(560, area.width), height: max(380, area.height + TitleBar.height + 4))
            model.tearOff(id, frame: TearOff.windowFrame(dropAt: m, size: size, screen: screen))
        }
        guard model?.motion != nil else { p.orderOut(nil); return }
        NSAnimationContext.runAnimationGroup({ c in
            c.duration = commit ? 0.12 : 0.18
            p.animator().alphaValue = 0
        }, completionHandler: { p.orderOut(nil) })
    }

    /// Each pane of the tab as it looks now, by block; empty when they aren't streaming (a tab
    /// not on screen): the card then shows their outlines.
    private func picture(of id: String, height: CGFloat) -> [String: CGImage] {
        guard let model, let renderer, let w = model.windows.first(where: { $0.id == id }) else { return [:] }
        let rects = model.visibleBlocks(in: w)
        guard !rects.isEmpty, rects.allSatisfy({ model.frames[$0.0] != nil }) else { return [:] }
        // Each at its size in the pane area, then scaled down by the card.
        let area = paneArea()
        let scale = NSScreen.main?.backingScaleFactor ?? 2
        var out: [String: CGImage] = [:]
        for (b, r) in rects {
            guard let f = model.frames[b] else { continue }
            let W = Int(r.width * area.width * scale), H = Int(r.height * area.height * scale)
            guard W > 0, H > 0 else { continue }
            let pad = Float(8 * scale)
            var d = PaneDraw(frame: f, origin: SIMD2(pad, pad), size: SIMD2(Float(W) - pad * 2, Float(H) - pad * 2), focused: b == w.focused)
            d.cursorShown = b == w.focused
            if let img = renderer.image([d], width: W, height: H) { out[b] = img }
        }
        return out
    }

    #if DEBUG
    /// Screenshot runs: the card for a tab, held at a point on screen.
    func showForShot(_ id: String, at point: CGPoint) {
        begin(id)
        panel?.setFrameOrigin(CGPoint(x: point.x - grip.x, y: point.y - ((panel?.frame.height ?? 0) - grip.y)))
    }
    #endif
}

/// The card under the pointer: a small window of the tab.
struct TearCard: View {
    @ObservedObject var model: AppModel
    let id: String
    let picture: [String: CGImage]
    let pictureHeight: CGFloat
    let pops: Bool
    @State private var shown = false

    var body: some View {
        let s = model.style
        let w = model.windows.first { $0.id == id }
        VStack(spacing: 0) {
            HStack(spacing: 7) {
                if let w {
                    ProgramTile(program: model.program(of: w), size: 16, behind: model.otherPrograms(of: w))
                    Text(w.titleParts(model.blocks).primary)
                        .font(.system(size: 12, weight: .medium)).foregroundStyle(s.text.swiftUI)
                        .lineLimit(1).fixedSize()
                        .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading).clipped().mask(FadeMask())
                }
            }
            .padding(.horizontal, 10)
            .frame(height: 32)
            // The panes as small cards, laid out as in the tab: their pictures when they stream,
            // else their outlines, so the card still shows the tab's shape.
            GeometryReader { g in
                if let w {
                    ForEach(model.visibleBlocks(in: w), id: \.0) { b, r in
                        let shape = RoundedRectangle(cornerRadius: 5, style: .continuous)
                        ZStack {
                            shape.fill(b == w.focused ? s.focusedPane.swiftUI : s.unfocusedPane.swiftUI)
                            if let img = picture[b] { Image(decorative: img, scale: 1).resizable().interpolation(.high) }
                        }
                        .clipShape(shape)
                        .overlay(shape.strokeBorder(b == w.focused ? s.accent.swiftUI.opacity(0.5) : s.border.swiftUI, lineWidth: 1))
                        .frame(width: max(0, r.width * g.size.width - 4), height: max(0, r.height * g.size.height - 4))
                        .position(x: r.midX * g.size.width, y: r.midY * g.size.height)
                    }
                }
            }
            .frame(height: pictureHeight)
            .padding([.horizontal, .bottom], 8)
        }
        .frame(width: TearDrag.cardWidth)
        .background(
            RoundedRectangle(cornerRadius: 10, style: .continuous).fill(s.windowBottom.swiftUI)
                .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(s.accent.swiftUI.opacity(0.55), lineWidth: 1))
                .shadow(color: .black.opacity(s.dark ? 0.45 : 0.22), radius: 14, y: 6)
        )
        .scaleEffect(shown || !pops ? 1 : 0.9, anchor: UnitPoint(x: 0.35, y: 0.05))
        .opacity(shown || !pops ? 1 : 0)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .onAppear { withAnimation(model.motion) { shown = true } }
    }
}
