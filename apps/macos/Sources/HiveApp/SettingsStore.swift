import AppKit
import Combine
import HiveCore

/// The settings file, as the app sees it. Reading and writing go through
/// the bundled `hive config`, so the app and the server read the file the
/// same way. The file is watched: saving it in any editor applies it.
@MainActor
final class SettingsStore: ObservableObject {
    @Published private(set) var settings: Settings
    /// Whether the file exists. Until it does, the app keeps the choices it
    /// remembered before settings had a file (UserDefaults).
    @Published private(set) var fileExists: Bool
    /// The last write that failed, such as a read-only dotfiles checkout.
    @Published var writeError: String?

    private var sources: [DispatchSourceFileSystemObject] = []
    private var pending: DispatchWorkItem?
    private let io = DispatchQueue(label: "hive.settings")

    init() {
        // Read once before the window exists, so it opens in the right theme.
        let s = Self.read() ?? Settings.defaults
        settings = s
        fileExists = !s.path.isEmpty && FileManager.default.fileExists(atPath: s.path)
        watch()
    }

    /// The file's path, as hive resolves it (HIVE_CONFIG, XDG, ~/.config).
    var path: String { settings.path }

    // MARK: hive config

    nonisolated static func run(_ args: [String]) -> (out: Data, err: String, status: Int32)? {
        guard let bin = AppModel.hiveBinary() else { return nil }
        let p = Process()
        p.executableURL = URL(fileURLWithPath: bin)
        p.arguments = args
        let out = Pipe(), err = Pipe()
        p.standardOutput = out
        p.standardError = err
        do { try p.run() } catch { return nil }
        let data = out.fileHandleForReading.readDataToEndOfFile()
        let errData = err.fileHandleForReading.readDataToEndOfFile()
        p.waitUntilExit()
        return (data, String(decoding: errData, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines), p.terminationStatus)
    }

    nonisolated static func read() -> Settings? {
        guard let r = run(["config", "show", "--json"]), r.status == 0 else { return nil }
        return try? Settings.decode(r.out)
    }

    /// Reads the file again, off the main thread.
    func reload() {
        io.async {
            let s = Self.read()
            Task { @MainActor in
                guard let s else { return }
                if s != self.settings { self.settings = s }
                let exists = FileManager.default.fileExists(atPath: s.path)
                if exists != self.fileExists { self.fileExists = exists }
                self.watch() // a rename-saving editor replaced the file
            }
        }
    }

    /// Changes one setting in the file, keeping the rest of it.
    func set(_ key: String, _ value: String) {
        io.async {
            let r = Self.run(["config", "set", key, value])
            Task { @MainActor in
                if let r, r.status == 0 {
                    self.writeError = nil
                } else {
                    self.writeError = r.map { $0.err.replacingOccurrences(of: "Error: ", with: "") } ?? "hive isn't installed with the app"
                }
                self.reload()
            }
        }
    }

    /// Opens the file in the user's editor, creating it from the template.
    func openInEditor() {
        io.async {
            // Creates the file from the template (every setting commented at
            // its default) if there's none; an existing one is left alone.
            let r = Self.run(["config", "path", "--create"])
            let path = r.flatMap { $0.status == 0 ? String(decoding: $0.out, as: UTF8.self) : nil }?
                .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            Task { @MainActor in
                if path.isEmpty { self.writeError = r?.err ?? "hive isn't installed with the app" }
                guard !path.isEmpty else { return }
                let url = URL(fileURLWithPath: path)
                // A file named "config" has no type: without an app for it,
                // use the text editor rather than asking.
                if NSWorkspace.shared.urlForApplication(toOpen: url) != nil {
                    NSWorkspace.shared.open(url)
                } else if let edit = NSWorkspace.shared.urlForApplication(withBundleIdentifier: "com.apple.TextEdit") {
                    NSWorkspace.shared.open([url], withApplicationAt: edit, configuration: NSWorkspace.OpenConfiguration())
                }
                self.reload()
            }
        }
    }

    // MARK: watching

    /// Watches the file and its folder (resolved through symlinks, so a
    /// stow-managed file is watched where it lives): in-place writes change
    /// the file, rename-saving editors change the folder.
    private func watch() {
        for s in sources { s.cancel() }
        sources = []
        guard !path.isEmpty else { return }
        let file = URL(fileURLWithPath: path).resolvingSymlinksInPath()
        var dir = file.deletingLastPathComponent()
        // No folder yet: watch the nearest one that exists, until it does.
        while !FileManager.default.fileExists(atPath: dir.path), dir.pathComponents.count > 1 {
            dir = dir.deletingLastPathComponent()
        }
        for (url, mask) in [(file, DispatchSource.FileSystemEvent([.write, .extend, .delete, .rename])),
                            (dir, DispatchSource.FileSystemEvent([.write]))] {
            let fd = open(url.path, O_EVTONLY)
            guard fd >= 0 else { continue }
            let src = DispatchSource.makeFileSystemObjectSource(fileDescriptor: fd, eventMask: mask, queue: .main)
            src.setEventHandler { [weak self] in self?.changed() }
            src.setCancelHandler { close(fd) }
            src.resume()
            sources.append(src)
        }
    }

    /// Coalesces a burst of events (editors write in steps) into one read.
    private func changed() {
        pending?.cancel()
        let w = DispatchWorkItem { [weak self] in self?.reload() }
        pending = w
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.15, execute: w)
    }
}
