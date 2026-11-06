import AppKit
import Combine
import HiveCore
import HiveRender
import SwiftUI

/// A tab in its own window: the same chrome as the main window, a title bar with the tab's
/// name and a button that returns it, and its panes. Closing the window returns the tab;
/// it never closes the tab.
@MainActor
final class TornWindow: NSObject, NSWindowDelegate {
    let id: String
    let window: NSWindow
    let panes: PanesView
    private let root: TornRoot
    private unowned let model: AppModel
    private var bag: Set<AnyCancellable> = []

    init(id: String, model: AppModel, renderer: Renderer, frame: NSRect) {
        self.id = id
        self.model = model
        window = NSWindow(contentRect: NSRect(origin: .zero, size: frame.size),
                          styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
                          backing: .buffered, defer: false)
        panes = PanesView(model: model, renderer: renderer, tab: id)
        root = TornRoot(frame: NSRect(origin: .zero, size: frame.size))
        super.init()
        window.isReleasedWhenClosed = false
        window.titlebarAppearsTransparent = true
        window.titleVisibility = .hidden
        let toolbar = NSToolbar(identifier: "hive-torn")
        toolbar.showsBaselineSeparator = false
        window.toolbar = toolbar
        window.toolbarStyle = .unifiedCompact
        window.minSize = NSSize(width: 480, height: 320)
        window.tabbingMode = .disallowed
        window.delegate = self
        let bar = TitleBarHost(rootView: TornBar(model: model, id: id))
        bar.safeAreaRegions = []
        root.addSubview(panes)
        root.addSubview(bar)
        window.contentView = root
        window.setFrame(frame, display: false)
        // The window's name is the tab's (Mission Control, the Window menu).
        model.$windows.combineLatest(model.$blocks).sink { [weak self] windows, blocks in
            guard let self, let w = windows.first(where: { $0.id == self.id }) else { return }
            let t = w.titleParts(blocks).primary
            self.window.title = t.isEmpty ? "hive" : t
        }.store(in: &bag)
        apply(model.style)
    }

    func apply(_ s: Style) {
        window.appearance = NSAppearance(named: s.dark ? .darkAqua : .aqua)
        window.backgroundColor = s.windowBottom
        root.gradient.colors = [s.windowTop.cgColor, s.windowBottom.cgColor]
        panes.restyle(s)
    }

    /// activate: it takes the keyboard; else it goes behind [below] (reopened at launch).
    func show(activate: Bool, below: NSWindow? = nil) {
        if activate {
            window.makeKeyAndOrderFront(nil)
        } else if let below {
            window.order(.below, relativeTo: below.windowNumber)
        } else {
            window.orderFront(nil)
        }
        panes.sync()
        // Not key yet, its panes still take this window's size (another device's hold stays).
        panes.claimSizes(force: true)
    }

    /// The app closes it (the tab came back); close() never asks windowShouldClose.
    func close() { window.close() }

    func windowDidBecomeKey(_ notification: Notification) {
        model.scope = id
        model.markSeen()
        panes.claimSizes(interactive: true)
        panes.sync()
    }

    func windowDidResignKey(_ notification: Notification) {
        // scope stays: a palette, sheet or alert over this window acts on its tab. The next
        // window with panes to become key sets it, and the tab coming back clears it.
        panes.sync()
        model.cancelKeySequence()
    }

    func windowShouldClose(_ sender: NSWindow) -> Bool {
        // The red button returns the tab to the main window, which then closes this one:
        // after AppKit is done asking, not from inside its close.
        DispatchQueue.main.async { [model, id] in model.bringBack(id) }
        return false
    }

    func windowDidMove(_ notification: Notification) { model.tornMoved(id, frame: window.frame) }
    func windowDidEndLiveResize(_ notification: Notification) { model.tornMoved(id, frame: window.frame) }
    func windowDidMiniaturize(_ notification: Notification) { model.windowChanged() }
    func windowDidDeminiaturize(_ notification: Notification) { model.windowChanged() }
    func windowDidChangeOcclusionState(_ notification: Notification) {
        if window.occlusionState.contains(.visible) { model.markSeen() }
    }
}

/// The torn window's content: the gradient, the bar, the panes under it.
final class TornRoot: NSView {
    let gradient = CAGradientLayer()
    override init(frame: NSRect) {
        super.init(frame: frame)
        wantsLayer = true
        layer?.addSublayer(gradient)
    }
    required init?(coder: NSCoder) { fatalError() }
    override var isFlipped: Bool { true }
    override func layout() {
        super.layout()
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        gradient.frame = bounds
        CATransaction.commit()
        for v in subviews {
            if v is PanesView {
                let top = TitleBar.height + 4
                v.frame = NSRect(x: 0, y: top, width: bounds.width, height: bounds.height - top)
            } else {
                v.frame = NSRect(x: 0, y: 0, width: bounds.width, height: TitleBar.height)
            }
        }
    }
}

/// The torn window's title bar: the tab as the tab bar shows it, and the way back.
struct TornBar: View {
    @ObservedObject var model: AppModel
    let id: String

    var body: some View {
        let s = model.style
        let w = model.windows.first { $0.id == id }
        HStack(spacing: 8) {
            if let w {
                let parts = w.titleParts(model.blocks)
                ProgramTile(program: model.program(of: w), size: 18, behind: model.otherPrograms(of: w))
                (Text(parts.primary).foregroundColor(s.text.swiftUI)
                    + Text(parts.secondary.isEmpty ? "" : " " + parts.secondary).foregroundColor(s.secondary.swiftUI.opacity(0.8)))
                    .font(.system(size: 12.5, weight: .medium))
                    .lineLimit(1).fixedSize()
                    .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
                    .clipped()
                    .mask(FadeMask())
                if let status = model.status(of: w) {
                    StatusBadge(state: status == "blocked" || status == "stopped" ? "blocked-dot" : status, style: s)
                }
            }
            Spacer(minLength: 4)
            BarButton(symbol: "arrow.uturn.backward", help: "Return to Main Window", style: s) { model.bringBack(id) }
                .accessibilityLabel("Return to Main Window")
        }
        .padding(.leading, TitleBar.leading)
        .padding(.trailing, 10)
        .frame(height: TitleBar.height)
        .contentShape(Rectangle())
    }
}
