import AppKit
import HiveCore
import SwiftUI

/// Hive → Settings… (⌘,): the common settings, written into the settings
/// file, which stays the source of truth. "Open Config File" has the rest.
@MainActor
final class SettingsWindow {
    private var window: NSWindow?

    func show(model: AppModel) {
        if let window {
            window.makeKeyAndOrderFront(nil)
            return
        }
        let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 540, height: 600),
                         styleMask: [.titled, .closable, .miniaturizable], backing: .buffered, defer: false)
        w.title = "Settings"
        w.isReleasedWhenClosed = false
        w.contentView = NSHostingView(rootView: SettingsView(model: model, store: model.settingsStore))
        w.center()
        w.setFrameAutosaveName("HiveSettings")
        w.makeKeyAndOrderFront(nil)
        window = w
    }
}

struct SettingsView: View {
    @ObservedObject var model: AppModel
    @ObservedObject var store: SettingsStore

    private var s: HiveCore.Settings { store.settings }

    /// A setting's help line, from hive itself.
    private func help(_ key: String) -> String {
        guard let h = s.keys.first(where: { $0.name == key })?.help else { return "" }
        return h.prefix(1).uppercased() + h.dropFirst() + "."
    }

    /// Writes a server setting into the file and asks the server to read it.
    private func setServer(_ key: String, _ value: String) {
        store.set(key, value)
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { model.call("config.reload") }
    }

    var body: some View {
        Form {
            Section("Appearance") {
                Picker("Theme", selection: Binding(get: { model.appearance }, set: { model.setAppearance($0) })) {
                    ForEach(Appearance.allCases, id: \.self) { Text($0.title).tag($0) }
                }
                .pickerStyle(.segmented)
                Picker("Font", selection: Binding(get: { s.fontFamily }, set: { store.set("font-family", $0) })) {
                    Text("JetBrains Mono (bundled)").tag("")
                    ForEach(MonospacedFonts.families, id: \.self) { Text($0).tag($0) }
                    if !s.fontFamily.isEmpty, !MonospacedFonts.families.contains(s.fontFamily) {
                        Text("\(s.fontFamily) (not installed)").tag(s.fontFamily)
                    }
                }
                Stepper(value: Binding(get: { s.fontSize }, set: { store.set("font-size", Self.number($0)) }),
                        in: 8...32, step: 0.5) {
                    HStack { Text("Font size"); Spacer(); Text("\(Self.number(s.fontSize)) pt").foregroundStyle(.secondary) }
                }
                Text("Font changes apply the next time hive opens.").font(.caption).foregroundStyle(.secondary)
            }
            Section("Terminal") {
                Picker("Bell", selection: Binding(get: { s.bell }, set: { store.set("bell", $0) })) {
                    Text("Flash the pane").tag("flash")
                    Text("Play a sound").tag("sound")
                    Text("Flash and play a sound").tag("flash+sound")
                    Text("Nothing").tag("none")
                }
                Toggle("Ask before pasting several lines", isOn: Binding(get: { s.confirmPaste }, set: { store.set("confirm-paste", $0 ? "true" : "false") }))
                Picker("Scrollback", selection: Binding(get: { s.scrollbackLimit }, set: { setServer("scrollback-limit", Self.size($0)) })) {
                    ForEach(Self.scrollbackChoices(current: s.scrollbackLimit), id: \.self) { Text(Self.size($0)).tag($0) }
                }
                Text(help("scrollback-limit")).font(.caption).foregroundStyle(.secondary)
            }
            Section("Agents") {
                Picker("Notifications", selection: Binding(get: { s.notify }, set: { store.set("notify", $0) })) {
                    Text("When hive is in the background").tag("background")
                    Text("Always").tag("always")
                    Text("Never").tag("never")
                }
                Toggle("Resume agents when the server restarts", isOn: Binding(get: { s.resumeAgents }, set: { setServer("resume-agents", $0 ? "true" : "false") }))
            }
            Section {
                if let problem = store.writeError.map({ "Settings: " + $0 }) ?? s.banner {
                    Label(problem, systemImage: "exclamationmark.triangle.fill").foregroundStyle(.red)
                }
                HStack {
                    Text(s.path.replacingOccurrences(of: NSHomeDirectory(), with: "~"))
                        .font(.system(.caption, design: .monospaced)).foregroundStyle(.secondary)
                        .lineLimit(1).truncationMode(.middle).textSelection(.enabled)
                    Spacer()
                    Button("Open Config File") { store.openInEditor() }
                }
            } footer: {
                Text("Everything here is saved in this file, and editing the file updates this window.")
                    .font(.caption).foregroundStyle(.secondary)
            }
        }
        .formStyle(.grouped)
        .frame(width: 540)
        .fixedSize(horizontal: false, vertical: true)
    }

    static func number(_ v: Double) -> String { v == v.rounded() ? String(Int(v)) : String(format: "%.1f", v) }

    static func size(_ b: UInt64) -> String {
        b % (1 << 30) == 0 ? "\(b >> 30)g" : b % (1 << 20) == 0 ? "\(b >> 20)m" : b % (1 << 10) == 0 ? "\(b >> 10)k" : "\(b)"
    }

    static func scrollbackChoices(current: UInt64) -> [UInt64] {
        var c: [UInt64] = [4 << 20, 16 << 20, 64 << 20, 256 << 20]
        if !c.contains(current) { c.append(current); c.sort() }
        return c
    }
}

/// The installed font families whose regular face is fixed-pitch.
@MainActor
enum MonospacedFonts {
    static let families: [String] = NSFontManager.shared.availableFontFamilies.filter { family in
        guard let first = NSFontManager.shared.availableMembers(ofFontFamily: family)?.first,
              let name = first.first as? String, let font = NSFont(name: name, size: 13) else { return false }
        return font.isFixedPitch
    }
}
