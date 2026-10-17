import AppKit
import HiveCore
import HiveRender
import MetalKit
import SwiftUI

/// One block's terminal: a Metal view drawing its mirror's frames, and the
/// keyboard input for it while it is focused.
final class PaneView: MTKView, NSTextInputClient {
    let blockID: String
    unowned let model: AppModel
    let renderer: Renderer
    var focused = false { didSet { if focused != oldValue { needsDisplay = true } } }
    var style: Style { model.style }

    private var marked = NSAttributedString()
    private var composedText: String?
    private var inKeyDown = false

    init(blockID: String, model: AppModel, renderer: Renderer) {
        self.blockID = blockID
        self.model = model
        self.renderer = renderer
        super.init(frame: .zero, device: renderer.device)
        colorPixelFormat = .bgra8Unorm
        framebufferOnly = true
        isPaused = true
        enableSetNeedsDisplay = true
        autoResizeDrawable = false
        layer?.isOpaque = true
    }

    required init(coder: NSCoder) { fatalError() }

    override var acceptsFirstResponder: Bool { true }
    override var isFlipped: Bool { true }

    var scale: CGFloat { window?.backingScaleFactor ?? 2 }
    var cellSize: CGSize { CGSize(width: CGFloat(renderer.fonts.cellWidth) / scale, height: CGFloat(renderer.fonts.cellHeight) / scale) }

    /// The grid that fits.
    var grid: (cols: Int, rows: Int) {
        (max(2, Int(bounds.width / cellSize.width)), max(2, Int(bounds.height / cellSize.height)))
    }

    override func setFrameSize(_ newSize: NSSize) {
        super.setFrameSize(newSize)
        let px = CGSize(width: (newSize.width * scale).rounded(), height: (newSize.height * scale).rounded())
        if px.width > 0, px.height > 0, drawableSize != px { drawableSize = px }
        needsDisplay = true
    }

    override func draw(_ dirtyRect: NSRect) {
        guard let drawable = currentDrawable, let cb = renderer.makeCommandBuffer() else { return }
        let bg = style.terminal.background
        var draws: [PaneDraw] = []
        if let frame = model.frames[blockID] {
            var d = PaneDraw(frame: frame, origin: .zero, size: SIMD2(Float(drawableSize.width), Float(drawableSize.height)),
                             focused: focused && (window?.isKeyWindow ?? false))
            if !focused {
                let c = style.cardUnfocused.usingColorSpace(.sRGB) ?? style.cardUnfocused
                let a = Float(style.unfocusedTint)
                d.tint = SIMD4(Float(c.redComponent) * a, Float(c.greenComponent) * a, Float(c.blueComponent) * a, a)
            }
            draws = [d]
        }
        renderer.encode(draws, into: drawable.texture, commandBuffer: cb, clear: bg)
        cb.present(drawable)
        cb.commit()
    }

    // MARK: mouse

    override func mouseDown(with event: NSEvent) {
        window?.makeFirstResponder(self)
        model.focus(block: blockID)
    }

    override func scrollWheel(with event: NSEvent) {
        let lines = event.hasPreciseScrollingDeltas ? event.scrollingDeltaY / cellSize.height : event.scrollingDeltaY * 3
        if abs(lines) >= 1 { model.scroll(block: blockID, rows: -Int(lines.rounded())) }
    }

    // MARK: keyboard

    override func becomeFirstResponder() -> Bool {
        model.focus(block: blockID)
        return true
    }

    override func keyDown(with event: NSEvent) {
        if event.modifierFlags.contains(.command) { super.keyDown(with: event); return }
        composedText = nil
        let hadMarked = marked.length > 0
        inKeyDown = true
        interpretKeyEvents([event])
        inKeyDown = false
        if marked.length > 0 || hadMarked && composedText == nil { return } // composing
        model.key(keyInput(event, text: composedText))
    }

    private func keyInput(_ event: NSEvent, text: String?) -> KeyInput {
        var mods = KeyMods()
        let f = event.modifierFlags
        if f.contains(.shift) { mods.insert(.shift) }
        if f.contains(.control) { mods.insert(.ctrl) }
        if f.contains(.option) { mods.insert(.alt) }
        if f.contains(.capsLock) { mods.insert(.capsLock) }
        var t = text ?? ""
        if t.unicodeScalars.contains(where: { $0.value < 0x20 || $0.value == 0x7F || (0xF700...0xF8FF).contains($0.value) }) { t = "" }
        return KeyInput(key: .mac(event.keyCode), mods: mods, text: t, unshifted: event.charactersIgnoringModifiers?.lowercased().first)
    }

    override func doCommand(by selector: Selector) {}

    @objc func paste(_ sender: Any?) {
        if let s = NSPasteboard.general.string(forType: .string) { model.paste(s) }
    }

    // MARK: NSTextInputClient

    func insertText(_ string: Any, replacementRange: NSRange) {
        let s = (string as? NSAttributedString)?.string ?? (string as? String) ?? ""
        marked = NSAttributedString()
        if inKeyDown { composedText = s } else { model.text(s) }
    }
    func setMarkedText(_ string: Any, selectedRange: NSRange, replacementRange: NSRange) {
        marked = (string as? NSAttributedString) ?? NSAttributedString(string: string as? String ?? "")
    }
    func unmarkText() { marked = NSAttributedString() }
    func selectedRange() -> NSRange { NSRange(location: NSNotFound, length: 0) }
    func markedRange() -> NSRange { marked.length > 0 ? NSRange(location: 0, length: marked.length) : NSRange(location: NSNotFound, length: 0) }
    func hasMarkedText() -> Bool { marked.length > 0 }
    func attributedSubstring(forProposedRange range: NSRange, actualRange: NSRangePointer?) -> NSAttributedString? { nil }
    func validAttributesForMarkedText() -> [NSAttributedString.Key] { [] }
    func characterIndex(for point: NSPoint) -> Int { NSNotFound }
    func firstRect(forCharacterRange range: NSRange, actualRange: NSRangePointer?) -> NSRect {
        let c = model.frames[blockID]?.cursor
        let r = NSRect(x: CGFloat(c?.x ?? 0) * cellSize.width, y: CGFloat((c?.y ?? 0) + 1) * cellSize.height, width: cellSize.width, height: cellSize.height)
        return window?.convertToScreen(convert(r, to: nil)) ?? r
    }
}

/// The header of a pane card: icon, title, and the focused pane's buttons.
struct PaneHeader: View {
    @ObservedObject var model: AppModel
    let blockID: String

    var body: some View {
        let info = model.blocks[blockID]
        let focused = model.focusedBlock == blockID
        let parts: (primary: String, secondary: String) = info?.titleParts ?? ("shell", "")
        let s = model.style
        HStack(spacing: 7) {
            Image(systemName: info?.symbol ?? "apple.terminal").font(.system(size: 11, weight: .semibold))
                .foregroundStyle(info?.harness == "claude" ? Color(red: 0.85, green: 0.47, blue: 0.34) : s.text.swiftUI)
            (Text(parts.primary).foregroundColor(s.text.swiftUI)
                + Text(parts.secondary.isEmpty ? "" : "  " + parts.secondary).foregroundColor(s.secondary.swiftUI).fontWeight(.regular))
                .font(.system(size: 12.5, weight: .semibold))
                .lineLimit(1).truncationMode(.middle)
            if let st = info?.agentState, st == "working" || st == "blocked" || st == "done" {
                StatusBadge(state: st, style: s)
            }
            if info?.exited == true {
                Text("Exited\(info!.exitCode != 0 ? " \(info!.exitCode)" : "")").font(.system(size: 10, weight: .medium))
                    .padding(.horizontal, 6).padding(.vertical, 1)
                    .background(Capsule().fill(s.secondary.swiftUI.opacity(0.18)))
                    .foregroundStyle(s.secondary.swiftUI)
            }
            Spacer(minLength: 8)
            if focused {
                HStack(spacing: 12) {
                    HeaderButton(symbol: "rectangle.split.2x1", help: "Split right (⌘D)") { model.split(horizontal: true) }
                    HeaderButton(symbol: "rectangle.split.1x2", help: "Split down (⇧⌘D)") { model.split(horizontal: false) }
                    HeaderButton(symbol: model.active?.zoomed == true ? "arrow.down.right.and.arrow.up.left" : "arrow.up.left.and.arrow.down.right",
                                 help: "Zoom (⇧⌘↩)") { model.toggleZoom() }
                    HeaderButton(symbol: "xmark", help: "Close (⌘W)") { model.closePane() }
                }
                .foregroundStyle(s.secondary.swiftUI)
            }
        }
        .opacity(focused ? 1 : 0.62)
        .padding(.horizontal, 12)
        .frame(height: 30)
        .contentShape(Rectangle())
        .onTapGesture { model.focus(block: blockID) }
    }
}

struct HeaderButton: View {
    let symbol: String
    let help: String
    let action: () -> Void
    @State private var hover = false
    var body: some View {
        Button(action: action) {
            Image(systemName: symbol).font(.system(size: 11.5, weight: .regular))
                .opacity(hover ? 1 : 0.8)
        }
        .buttonStyle(.plain)
        .help(help)
        .onHover { hover = $0 }
    }
}

/// Agent status as a small hexagon: honey and pulsing while working, coral
/// with a label when blocked, sage when done.
struct StatusBadge: View {
    let state: String
    var style: Style = .dark
    @State private var pulse = false
    var body: some View {
        switch state {
        case "blocked":
            HStack(spacing: 4) {
                Image(systemName: "hexagon.fill").font(.system(size: 8)).foregroundStyle(style.blocked.swiftUI)
                Text("Blocked").font(.system(size: 10, weight: .semibold)).foregroundStyle(style.blocked.swiftUI)
            }
            .padding(.horizontal, 6).padding(.vertical, 1)
            .background(Capsule().fill(style.blocked.swiftUI.opacity(0.14)))
        case "blocked-dot":
            Image(systemName: "hexagon.fill").font(.system(size: 8)).foregroundStyle(style.blocked.swiftUI)
        case "done":
            Image(systemName: "hexagon.fill").font(.system(size: 8)).foregroundStyle(style.done.swiftUI)
        default:
            Image(systemName: "hexagon.fill").font(.system(size: 8)).foregroundStyle(style.accent.swiftUI)
                .opacity(pulse ? 0.35 : 1)
                .animation(.easeInOut(duration: 0.9).repeatForever(autoreverses: true), value: pulse)
                .onAppear { pulse = true }
        }
    }
}

/// A rounded card holding a pane's header and terminal.
final class CardView: NSView {
    let pane: PaneView
    let header: NSHostingView<PaneHeader>
    let contentInset: CGFloat = 10

    init(pane: PaneView, model: AppModel) {
        self.pane = pane
        header = NSHostingView(rootView: PaneHeader(model: model, blockID: pane.blockID))
        super.init(frame: .zero)
        wantsLayer = true
        layer?.cornerRadius = 10
        layer?.cornerCurve = .continuous
        layer?.masksToBounds = true
        layer?.borderWidth = 1
        addSubview(header)
        addSubview(pane)
    }

    required init?(coder: NSCoder) { fatalError() }
    override var isFlipped: Bool { true }

    func apply(style: Style, focused: Bool) {
        layer?.backgroundColor = (focused ? style.focusedPane : style.unfocusedPane).cgColor
        layer?.borderColor = (focused ? style.accent.withAlphaComponent(0.55) : style.border.withAlphaComponent(0.7)).cgColor
        pane.focused = focused
    }

    override func layout() {
        super.layout()
        header.frame = NSRect(x: 0, y: 0, width: bounds.width, height: 30)
        // Whole cells only: the rest is card padding.
        let avail = NSRect(x: contentInset, y: 32, width: bounds.width - contentInset * 2, height: bounds.height - 32 - contentInset)
        let cw = pane.cellSize.width, ch = pane.cellSize.height
        let w = max(cw * 2, (avail.width / cw).rounded(.down) * cw)
        let h = max(ch * 2, (avail.height / ch).rounded(.down) * ch)
        pane.frame = NSRect(x: avail.minX, y: avail.minY, width: w, height: h)
    }
}

/// Lays the active window's panes out as cards.
final class PanesView: NSView {
    unowned let model: AppModel
    let renderer: Renderer
    private var cards: [String: CardView] = [:]
    let gap: CGFloat = 8

    init(model: AppModel, renderer: Renderer) {
        self.model = model
        self.renderer = renderer
        super.init(frame: .zero)
    }

    required init?(coder: NSCoder) { fatalError() }
    override var isFlipped: Bool { true }

    func card(_ id: String) -> CardView? { cards[id] }

    /// Rebuilds cards for the visible blocks and lays them out.
    func sync() {
        let visible = model.visibleBlocks
        let ids = Set(visible.map(\.0))
        for (id, c) in cards where !ids.contains(id) {
            c.removeFromSuperview()
            cards[id] = nil
        }
        for (id, _) in visible where cards[id] == nil {
            let c = CardView(pane: PaneView(blockID: id, model: model, renderer: renderer), model: model)
            addSubview(c)
            cards[id] = c
        }
        needsLayout = true
        layoutSubtreeIfNeeded()
        let focused = model.focusedBlock
        for (id, c) in cards {
            c.apply(style: model.style, focused: id == focused)
            c.pane.needsDisplay = true
        }
        if let f = focused, let c = cards[f], window?.firstResponder !== c.pane {
            window?.makeFirstResponder(c.pane)
        }
    }

    override func layout() {
        super.layout()
        let area = bounds.insetBy(dx: gap, dy: 0)
        let areaRect = NSRect(x: area.minX, y: 0, width: area.width, height: bounds.height - gap)
        for (id, f) in model.visibleBlocks {
            guard let c = cards[id] else { continue }
            var r = NSRect(x: areaRect.minX + f.minX * areaRect.width, y: areaRect.minY + f.minY * areaRect.height,
                           width: f.width * areaRect.width, height: f.height * areaRect.height)
            if f.minX > 0.0001 { r.origin.x += gap / 2; r.size.width -= gap / 2 }
            if f.maxX < 0.9999 { r.size.width -= gap / 2 }
            if f.minY > 0.0001 { r.origin.y += gap / 2; r.size.height -= gap / 2 }
            if f.maxY < 0.9999 { r.size.height -= gap / 2 }
            c.frame = r.integral
        }
    }

    override func setFrameSize(_ newSize: NSSize) {
        super.setFrameSize(newSize)
        claimSizes()
    }

    /// Takes size authority for the visible panes at their current sizes:
    /// on key focus and layout changes, never from a collapsed window.
    func claimSizes() {
        guard window?.isKeyWindow ?? false, bounds.width >= 200, bounds.height >= 100 else { return }
        layoutSubtreeIfNeeded()
        var sizes: [String: (cols: Int, rows: Int)] = [:]
        for (id, c) in cards { sizes[id] = c.pane.grid }
        model.claimSizes(sizes, cellWidth: renderer.fonts.cellWidth, cellHeight: renderer.fonts.cellHeight)
    }

    /// The size a new split of the focused pane will get.
    func splitSize(horizontal: Bool) -> (Int, Int)? {
        guard let f = model.focusedBlock, let p = cards[f]?.pane else { return nil }
        let g = p.grid
        return horizontal ? (max(2, (g.cols - 3) / 2), g.rows) : (g.cols, max(2, (g.rows - 2) / 2))
    }

    func redraw(_ block: String) { cards[block]?.pane.needsDisplay = true }
}
