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
    var focused = false { didSet { if focused != oldValue { needsDisplay = true; resetBlink() } } }
    var style: Style { model.style }

    private var marked = NSAttributedString() { didSet { showPreedit() } }
    /// The accent or IME text being composed, drawn at the cursor until it's
    /// committed, as Ghostty draws it: underlined, in the terminal's colours.
    private let preedit = PreeditView()
    /// What the input method inserted during this key press.
    private var committed: [String] = []
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
        preedit.isHidden = true
        addSubview(preedit)
    }

    private func showPreedit() {
        // Hidden when the program hides its cursor or it's scrolled away.
        guard marked.length > 0, let c = model.frames[blockID]?.cursor, c.visible else { preedit.isHidden = true; return }
        let t = style.terminal
        func color(_ c: RGB) -> NSColor { NSColor(srgbRed: CGFloat(c.r) / 255, green: CGFloat(c.g) / 255, blue: CGFloat(c.b) / 255, alpha: 1) }
        let font = NSFont(descriptor: CTFontCopyFontDescriptor(renderer.fonts.regular), size: CTFontGetSize(renderer.fonts.regular) / scale)
            ?? .monospacedSystemFont(ofSize: 13, weight: .regular)
        preedit.text = NSAttributedString(string: marked.string, attributes: [.font: font, .foregroundColor: color(t.foreground)])
        preedit.background = color(t.background)
        preedit.baseline = (CGFloat(renderer.fonts.cellHeight) - renderer.fonts.baseline) / scale
        let width = max(cellSize.width, ceil(preedit.text.size().width))
        preedit.frame = NSRect(x: CGFloat(c.x) * cellSize.width, y: CGFloat(c.y - rowOffset) * cellSize.height, width: width, height: cellSize.height)
        preedit.isHidden = false
        preedit.needsDisplay = true
    }

    required init(coder: NSCoder) { fatalError() }

    // MARK: cursor blink

    /// Ghostty's blink, while this is the focused pane of the key window and
    /// the program's cursor blinks (the default; see cursor-style-blink).
    private var blink = CursorBlink(at: 0)
    private var blinkTimer: Timer?
    private var now: TimeInterval { ProcessInfo.processInfo.systemUptime }

    private var blinks: Bool {
        guard focused, window?.isKeyWindow == true, let c = model.frames[blockID]?.cursor else { return false }
        return c.blinking && c.visible
    }

    /// Shows the cursor at once and starts the blink again.
    func resetBlink() {
        blink.reset(at: now)
        scheduleBlink()
    }

    /// Output shows the cursor too, at most every 500 ms as in Ghostty: a
    /// program that keeps drawing keeps a steady cursor.
    func outputArrived() {
        if now - blink.since > 0.5 { resetBlink() }
    }

    private func scheduleBlink() {
        blinkTimer?.invalidate()
        blinkTimer = nil
        guard blinks else { return }
        let t = Timer(timeInterval: max(0.01, blink.nextChange(after: now) - now), repeats: false) { [weak self] _ in
            MainActor.assumeIsolated {
                self?.needsDisplay = true
                self?.scheduleBlink()
            }
        }
        RunLoop.main.add(t, forMode: .common)
        blinkTimer = t
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        if window == nil { blinkTimer?.invalidate(); blinkTimer = nil }
    }

    override var acceptsFirstResponder: Bool { true }
    override var isFlipped: Bool { true }

    var scale: CGFloat { window?.backingScaleFactor ?? 2 }
    var cellSize: CGSize { CGSize(width: CGFloat(renderer.fonts.cellWidth) / scale, height: CGFloat(renderer.fonts.cellHeight) / scale) }

    /// Rows of the grid hidden above the pane, when another window owns a
    /// taller size: the cursor's row stays in view (see Viewport).
    var rowOffset: Int {
        guard let f = model.frames[blockID] else { return 0 }
        return Viewport.rowOffset(rows: f.rowsCount, visible: Int(bounds.height / cellSize.height), cursorRow: f.cursor.y)
    }

    /// Pointer geometry over the whole grid, which can be taller than the pane.
    private var pointerGeometry: PointerGeometry {
        let gridHeight = (model.frames[blockID]?.rowsCount ?? 0) * renderer.fonts.cellHeight
        return PointerGeometry(cellWidth: renderer.fonts.cellWidth, cellHeight: renderer.fonts.cellHeight,
                               paneWidth: Int(drawableSize.width), paneHeight: max(Int(drawableSize.height), gridHeight))
    }

    /// A point in the pane, in the grid's points (below any hidden rows).
    private func gridPoint(_ event: NSEvent) -> NSPoint {
        let p = convert(event.locationInWindow, from: nil)
        return NSPoint(x: p.x, y: p.y + CGFloat(rowOffset) * cellSize.height)
    }

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
            d.scrollY = Float(rowOffset * renderer.fonts.cellHeight)
            // The key window or the program's blink may have changed since.
            if blinks != (blinkTimer != nil) { resetBlink() }
            d.cursorShown = !blinks || blink.visible(at: now)
            // Composing text follows the cursor as the program redraws.
            if marked.length > 0 { showPreedit() }
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
        pointer(.press, .left, event)
    }

    override func mouseDragged(with event: NSEvent) { pointer(.motion, .left, event) }
    override func mouseUp(with event: NSEvent) { pointer(.release, .left, event) }
    /// A right-click is the program's when it uses the mouse; otherwise, or
    /// with Shift, it opens hive's menu for the pane.
    override func rightMouseDown(with event: NSEvent) {
        if event.modifierFlags.contains(.shift) || !model.tracksMouse(block: blockID) {
            window?.makeFirstResponder(self)
            model.focus(block: blockID)
            NSMenu.popUpContextMenu(contextMenu(), with: event, for: self)
            return
        }
        pointer(.press, .right, event)
    }
    override func rightMouseUp(with event: NSEvent) { pointer(.release, .right, event) }

    #if DEBUG
    /// The screenshot loop: the context menu, as a right-click opens it.
    func showContextMenuForTesting() {
        contextMenu().popUp(positioning: nil, at: NSPoint(x: bounds.midX, y: bounds.midY), in: self)
    }
    #endif

    private func contextMenu() -> NSMenu {
        let m = NSMenu()
        m.addItem(NSMenuItem(title: "Copy", action: #selector(copy(_:)), keyEquivalent: "c"))
        m.addItem(NSMenuItem(title: "Paste", action: #selector(paste(_:)), keyEquivalent: "v"))
        m.addItem(.separator())
        for item in model.contextActions?() ?? [] { m.addItem(item) }
        return m
    }
    override func otherMouseDown(with event: NSEvent) { pointer(.press, .middle, event) }
    override func otherMouseUp(with event: NSEvent) { pointer(.release, .middle, event) }
    override func mouseMoved(with event: NSEvent) { pointer(.motion, nil, event) }

    // Hover reaches programs that track all motion (Claude Code's links).
    private var hoverArea: NSTrackingArea?
    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        if let hoverArea { removeTrackingArea(hoverArea) }
        let area = NSTrackingArea(rect: bounds, options: [.mouseMoved, .activeInKeyWindow, .inVisibleRect], owner: self)
        addTrackingArea(area)
        hoverArea = area
    }

    /// The mouse goes to the program when it tracks it; otherwise it drives
    /// selection in the mirror through libghostty's gestures: drag,
    /// double-click for a word, triple-click for a line. Shift always selects.
    private func pointer(_ action: Mirror.PointerAction, _ button: Mirror.PointerButton?, _ event: NSEvent) {
        let p = gridPoint(event)
        var mods = KeyMods()
        let f = event.modifierFlags
        if f.contains(.shift) { mods.insert(.shift) }
        if f.contains(.control) { mods.insert(.ctrl) }
        if f.contains(.option) { mods.insert(.alt) }
        let geometry = pointerGeometry
        model.pointer(block: blockID, action, button: button, mods: mods, x: Double(p.x * scale), y: Double(p.y * scale),
                      geometry: geometry, col: Int(p.x / cellSize.width), row: Int(p.y / cellSize.height),
                      repeatInterval: NSEvent.doubleClickInterval, repeatDistance: Double(4 * scale))
    }

    @objc func copy(_ sender: Any?) { model.copySelection(block: blockID) }

    /// Trackpad movement smaller than a line, kept until it adds up to one.
    private var wheelRemainder: CGFloat = 0

    override func scrollWheel(with event: NSEvent) {
        if event.phase == .began || event.phase == .mayBegin { wheelRemainder = 0 }
        let delta = event.hasPreciseScrollingDeltas ? event.scrollingDeltaY / cellSize.height : event.scrollingDeltaY * 3
        wheelRemainder += delta
        let lines = Int(wheelRemainder.rounded(.towardZero))
        guard lines != 0 else { return }
        wheelRemainder -= CGFloat(lines)
        let p = gridPoint(event)
        let geometry = pointerGeometry
        // Wheel up (positive delta) scrolls back: negative rows.
        model.wheel(block: blockID, rows: -lines, shift: event.modifierFlags.contains(.shift),
                    x: Double(p.x * scale), y: Double(p.y * scale), geometry: geometry)
    }

    // MARK: keyboard

    override func becomeFirstResponder() -> Bool {
        model.focus(block: blockID)
        return true
    }

    override func keyDown(with event: NSEvent) {
        // ⌘←, ⌘→, ⌘⌫, ⌥←, ⌥→ and ⇧↩, as Ghostty sends them (see MacBindings).
        var mods = KeyMods()
        let f = event.modifierFlags
        if f.contains(.shift) { mods.insert(.shift) }
        if f.contains(.control) { mods.insert(.ctrl) }
        if f.contains(.option) { mods.insert(.alt) }
        if f.contains(.command) { mods.insert(.command) }
        resetBlink()
        if let text = MacBindings.text(key: .mac(event.keyCode), mods: mods) {
            model.text(text)
            return
        }
        if event.modifierFlags.contains(.command) { super.keyDown(with: event); return }
        committed = []
        let hadMarked = marked.length > 0
        inKeyDown = true
        interpretKeyEvents([event])
        inKeyDown = false
        let o = Composition.decide(hadMarked: hadMarked, markedNow: marked.length > 0, committed: committed,
                                   key: .mac(event.keyCode), mods: mods)
        for t in o.texts { model.key(.committed(t)) }
        for t in o.keyTexts { model.key(keyInput(event, text: t)) }
        if o.plainKey { model.key(keyInput(event, text: nil)) }
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
        guard let s = NSPasteboard.general.string(forType: .string), !s.isEmpty else { return }
        model.pasteChecked(block: blockID, s) { [weak self] safe in
            guard !safe, let window = self?.window else { self?.model.paste(s); return }
            // Outside bracketed paste every line runs as soon as it arrives.
            let alert = NSAlert()
            let lines = s.split(whereSeparator: \.isNewline).count
            alert.messageText = "Paste \(lines) line\(lines == 1 ? "" : "s")?"
            alert.informativeText = "This program didn't ask for bracketed paste, so each line runs as soon as it's pasted."
            alert.addButton(withTitle: "Paste")
            alert.addButton(withTitle: "Cancel")
            alert.beginSheetModal(for: window) { r in
                if r == .alertFirstButtonReturn { self?.model.paste(s) }
            }
        }
    }

    // MARK: NSTextInputClient

    func insertText(_ string: Any, replacementRange: NSRange) {
        let s = (string as? NSAttributedString)?.string ?? (string as? String) ?? ""
        marked = NSAttributedString()
        // A dead key then a consonant commits twice ("~", then "h").
        if inKeyDown { committed.append(s) } else { model.text(s) }
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
        let r = NSRect(x: CGFloat(c?.x ?? 0) * cellSize.width, y: CGFloat((c?.y ?? 0) - rowOffset + 1) * cellSize.height, width: cellSize.width, height: cellSize.height)
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
            ProgramGlyph(program: info?.programInfo ?? Programs.lookup(""), color: s.text.swiftUI.opacity(0.9), size: 13)
                .accessibilityHidden(true)  // the title beside it names the pane
            // Like Ghostty's 🔔 before the title: the pane rang, until it's
            // focused again or typed into.
            if model.bellMarks.contains(blockID) { BellMark(style: s) }
            (Text(parts.primary).foregroundColor(s.text.swiftUI)
                + Text(parts.secondary.isEmpty ? "" : "  " + parts.secondary).foregroundColor(s.secondary.swiftUI).fontWeight(.regular))
                .font(.system(size: 12.5, weight: .semibold))
                .lineLimit(1).truncationMode(.middle)
            if let st = info?.agentState, st == "working" || st == "blocked" || st == "done" {
                StatusBadge(state: st, style: s)
            }
            if model.copiedBlock == blockID {
                Text("Copied").font(.system(size: 10, weight: .medium))
                    .padding(.horizontal, 6).padding(.vertical, 1)
                    .background(Capsule().fill(s.accent.swiftUI.opacity(0.16)))
                    .foregroundStyle(s.accent.swiftUI)
                    .transition(.opacity)
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

    /// A brief honey flash over the pane: its program rang the bell.
    func flash() {
        guard let layer else { return }
        let f = CALayer()
        f.frame = layer.bounds
        f.backgroundColor = NSColor(srgbRed: 0xF2 / 255, green: 0xA9 / 255, blue: 0x3B / 255, alpha: 1).cgColor
        f.opacity = 0
        layer.addSublayer(f)
        let a = CABasicAnimation(keyPath: "opacity")
        a.fromValue = 0.22
        a.toValue = 0
        a.duration = 0.35
        a.timingFunction = CAMediaTimingFunction(name: .easeOut)
        CATransaction.begin()
        CATransaction.setCompletionBlock { f.removeFromSuperlayer() }
        f.add(a, forKey: "flash")
        CATransaction.commit()
    }

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

    /// Applies a theme to every card. Takes the style explicitly: a caller
    /// reacting to model.$style runs before model.style changes.
    func restyle(_ s: Style) {
        let focused = model.focusedBlock
        for (id, c) in cards {
            c.apply(style: s, focused: id == focused)
            c.pane.needsDisplay = true
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
    func claimSizes(force: Bool = false) {
        guard force || window?.isKeyWindow ?? false, bounds.width >= 200, bounds.height >= 100 else { return }
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

    func redraw(_ block: String) {
        guard let p = cards[block]?.pane else { return }
        p.outputArrived()
        p.needsDisplay = true
    }

    /// The font changed: every pane's grid changes size, so lay out again and
    /// claim the new sizes once.
    func fontChanged() {
        sync()
        for c in cards.values { c.pane.needsDisplay = true }
        // A deliberate local change: claim even if this window isn't key
        // (the Settings window usually is).
        claimSizes(force: true)
    }
}

/// Composing text over the cursor's cell: the cell's background, the text on
/// the grid's baseline, and an underline across it.
final class PreeditView: NSView {
    var text = NSAttributedString()
    var background = NSColor.black
    /// From the cell's top, in points.
    var baseline: CGFloat = 0

    override var isFlipped: Bool { true }

    override func draw(_ dirtyRect: NSRect) {
        background.setFill()
        bounds.fill()
        let font = text.attribute(.font, at: 0, effectiveRange: nil) as? NSFont
        text.draw(at: NSPoint(x: 0, y: baseline - (font?.ascender ?? 0)))
        (text.attribute(.foregroundColor, at: 0, effectiveRange: nil) as? NSColor ?? .white).setFill()
        NSRect(x: 0, y: baseline + 2, width: bounds.width, height: 1).fill()
    }
}
