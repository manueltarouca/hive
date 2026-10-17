import AppKit
import ImageIO
import HiveCore
import HiveRender
import MetalKit

/// Draws the active window's panes and turns keyboard input into terminal
/// input. One view per window: one drawable, one input client.
final class TerminalView: MTKView, NSTextInputClient {
    let model: AppModel
    private(set) var renderer: Renderer
    private let padding: CGFloat = 6   // points
    private let gap: CGFloat = 1       // points between panes
    private var paneRects: [(String, CGRect)] = [] // in view points, flipped (top-left origin)
    private var debugDumped = false

    // IME state for the current keyDown.
    private var marked = NSAttributedString()
    private var composedText: String?
    private var inKeyDown = false

    init(model: AppModel) {
        self.model = model
        let device = MTLCreateSystemDefaultDevice()!
        renderer = try! Renderer(device: device, fonts: FontSet(pointSize: 13, scale: NSScreen.main?.backingScaleFactor ?? 2))
        super.init(frame: NSRect(x: 0, y: 0, width: 900, height: 600), device: device)
        colorPixelFormat = .bgra8Unorm
        framebufferOnly = true
        // Draw on demand, never continuously.
        isPaused = true
        enableSetNeedsDisplay = true
        // Sized by hand: with constraints-driven layout the automatic
        // drawable size lagged the view and the picture was stretched.
        autoResizeDrawable = false
        let bg = renderer.theme.background
        clearColor = MTLClearColor(red: Double(bg.r) / 255, green: Double(bg.g) / 255, blue: Double(bg.b) / 255, alpha: 1)
        model.onFrame = { [weak self] in self?.needsDisplay = true }
        model.onLayout = { [weak self] in self?.claimSizes() }
    }

    required init(coder: NSCoder) { fatalError() }

    override var acceptsFirstResponder: Bool { true }
    override var isFlipped: Bool { true }

    override func viewDidChangeBackingProperties() {
        super.viewDidChangeBackingProperties()
        syncDrawableSize()
        let scale = window?.backingScaleFactor ?? 2
        if scale != renderer.fonts.scale, let r = try? Renderer(device: device, fonts: FontSet(pointSize: 13, scale: scale)) {
            renderer = r
        }
        claimSizes()
    }

    override func setFrameSize(_ newSize: NSSize) {
        super.setFrameSize(newSize)
        syncDrawableSize()
        claimSizes()
    }

    override func layout() {
        super.layout()
        syncDrawableSize()
    }

    private func syncDrawableSize() {
        let px = CGSize(width: (bounds.width * scale).rounded(), height: (bounds.height * scale).rounded())
        if px.width > 0, px.height > 0, drawableSize != px {
            drawableSize = px
            needsDisplay = true
        }
    }

    // MARK: layout

    private var scale: CGFloat { window?.backingScaleFactor ?? 2 }
    private var cellSize: CGSize { CGSize(width: CGFloat(renderer.fonts.cellWidth) / scale, height: CGFloat(renderer.fonts.cellHeight) / scale) }

    /// Pane rects in points, snapped so each holds whole cells.
    private func layoutPanes() -> [(String, CGRect)] {
        let area = bounds.insetBy(dx: padding, dy: padding)
        return model.visibleBlocks.map { (id, f) in
            var r = CGRect(x: area.minX + f.minX * area.width, y: area.minY + f.minY * area.height,
                           width: f.width * area.width, height: f.height * area.height)
            if f.minX > 0 { r.origin.x += gap; r.size.width -= gap }
            if f.minY > 0 { r.origin.y += gap; r.size.height -= gap }
            return (id, r.integral)
        }
    }

    private func gridSize(_ r: CGRect) -> (cols: Int, rows: Int) {
        (max(2, Int(r.width / cellSize.width)), max(2, Int(r.height / cellSize.height)))
    }

    /// Takes size authority for the visible panes, at their current sizes.
    func claimSizes() {
        // Not while the window is still being laid out: a collapsed view
        // would shrink every program to a few columns.
        guard window?.isKeyWindow ?? false, bounds.width >= 200, bounds.height >= 100 else { return }
        var sizes: [String: (cols: Int, rows: Int)] = [:]
        for (id, r) in layoutPanes() { sizes[id] = gridSize(r) }
        model.claimSizes(sizes, cellWidth: renderer.fonts.cellWidth, cellHeight: renderer.fonts.cellHeight)
    }

    /// The size a new split of the focused pane will have, so it starts at
    /// the right size instead of reflowing at once.
    func splitSize(horizontal: Bool) -> (Int, Int)? {
        guard let f = model.focusedBlock, let r = layoutPanes().first(where: { $0.0 == f })?.1 else { return nil }
        let g = gridSize(horizontal ? CGRect(x: 0, y: 0, width: r.width / 2 - gap, height: r.height)
                                    : CGRect(x: 0, y: 0, width: r.width, height: r.height / 2 - gap))
        return (g.cols, g.rows)
    }

    // MARK: drawing

    override func draw(_ dirtyRect: NSRect) {
        guard let drawable = currentDrawable, let cb = renderer.makeCommandBuffer() else { return }
        paneRects = layoutPanes()
        if ProcessInfo.processInfo.environment["HIVE_DEBUG"] != nil {
            NSLog("hive bounds %@ frame %@ drawable %@ layer %@", NSStringFromRect(bounds), NSStringFromRect(frame),
                  NSStringFromSize(drawableSize), NSStringFromRect(layer?.frame ?? .zero))
        }
        let s = Float(scale)
        let draws: [PaneDraw] = paneRects.compactMap { id, r in
            guard let frame = model.frames[id] else { return nil }
            return PaneDraw(frame: frame, origin: SIMD2(Float(r.minX) * s, Float(r.minY) * s),
                            size: SIMD2(Float(r.width) * s, Float(r.height) * s),
                            focused: id == model.focusedBlock && (window?.isKeyWindow ?? false))
        }
        if ProcessInfo.processInfo.environment["HIVE_DEBUG"] != nil, !draws.isEmpty, !debugDumped {
            debugDumped = true
            NSLog("hive draw: drawable %dx%d scale %.1f cell %dx%d panes %@", drawable.texture.width, drawable.texture.height,
                  scale, renderer.fonts.cellWidth, renderer.fonts.cellHeight, draws.map { "\($0.origin) \($0.size) \($0.frame.cols)x\($0.frame.rows)" }.description)
            if let img = renderer.image(draws, width: drawable.texture.width, height: drawable.texture.height),
               let dest = CGImageDestinationCreateWithURL(URL(fileURLWithPath: "/tmp/hive-offscreen.png") as CFURL, "public.png" as CFString, 1, nil) {
                CGImageDestinationAddImage(dest, img, nil)
                CGImageDestinationFinalize(dest)
            }
        }
        renderer.encode(draws, into: drawable.texture, commandBuffer: cb, clear: dividerColor)
        cb.present(drawable)
        cb.commit()
    }

    /// The gaps between panes show the clear color: a touch lighter than
    /// the background.
    private var dividerColor: RGB {
        let b = renderer.theme.background
        return paneRects.count > 1 ? RGB(b.r &+ 0x18, b.g &+ 0x18, b.b &+ 0x18) : b
    }

    // MARK: mouse

    override func mouseDown(with event: NSEvent) {
        window?.makeFirstResponder(self)
        let p = convert(event.locationInWindow, from: nil)
        if let (id, _) = paneRects.first(where: { $0.1.contains(p) }) {
            model.focus(block: id)
            claimSizes()
        }
    }

    override func scrollWheel(with event: NSEvent) {
        let p = convert(event.locationInWindow, from: nil)
        guard let (id, _) = paneRects.first(where: { $0.1.contains(p) }) else { return }
        let lines = event.hasPreciseScrollingDeltas ? event.scrollingDeltaY / cellSize.height : event.scrollingDeltaY * 3
        if abs(lines) >= 1 { model.scroll(block: id, rows: -Int(lines.rounded())) }
    }

    // MARK: keyboard

    override func keyDown(with event: NSEvent) {
        // Command combinations belong to the menus.
        if event.modifierFlags.contains(.command) { super.keyDown(with: event); return }
        claimSizesOnce()
        composedText = nil
        let hadMarked = marked.length > 0
        inKeyDown = true
        interpretKeyEvents([event])
        inKeyDown = false
        if marked.length > 0 || hadMarked && composedText == nil { needsDisplay = true; return } // composing
        model.key(keyInput(event, text: composedText))
    }

    private var claimedOnce = false
    private func claimSizesOnce() {
        if !claimedOnce { claimedOnce = true; claimSizes() }
    }

    private func keyInput(_ event: NSEvent, text: String?) -> KeyInput {
        var mods = KeyMods()
        let f = event.modifierFlags
        if f.contains(.shift) { mods.insert(.shift) }
        if f.contains(.control) { mods.insert(.ctrl) }
        if f.contains(.option) { mods.insert(.alt) }
        if f.contains(.capsLock) { mods.insert(.capsLock) }
        // Text only for printable input; control keys are encoded from the key.
        var t = text ?? ""
        if t.unicodeScalars.contains(where: { $0.value < 0x20 || $0.value == 0x7F || (0xF700...0xF8FF).contains($0.value) }) { t = "" }
        let unshifted = event.charactersIgnoringModifiers?.lowercased().first
        return KeyInput(key: .mac(event.keyCode), mods: mods, text: t, unshifted: unshifted)
    }

    override func doCommand(by selector: Selector) {
        // Keys like return and arrows arrive here; keyDown encodes them.
    }

    override func flagsChanged(with event: NSEvent) {}

    @objc func paste(_ sender: Any?) {
        if let s = NSPasteboard.general.string(forType: .string) { model.paste(s) }
    }

    @objc func copy(_ sender: Any?) {
        // Selection arrives with client-side selection; nothing to copy yet.
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

    /// Where the IME candidate window goes: at the cursor.
    func firstRect(forCharacterRange range: NSRange, actualRange: NSRangePointer?) -> NSRect {
        guard let f = model.focusedBlock, let frame = model.frames[f], let r = paneRects.first(where: { $0.0 == f })?.1 else {
            return window?.convertToScreen(convert(bounds, to: nil)) ?? .zero
        }
        let p = CGPoint(x: r.minX + CGFloat(frame.cursor.x) * cellSize.width, y: r.minY + CGFloat(frame.cursor.y + 1) * cellSize.height)
        let inWindow = convert(NSRect(origin: p, size: cellSize), to: nil)
        return window?.convertToScreen(inWindow) ?? inWindow
    }
}
