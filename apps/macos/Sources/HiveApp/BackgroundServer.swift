import Foundation
import ServiceManagement

/// The hive server as Hive's background item (Login Items & Extensions): macOS
/// stops what an app started when that app closes, unless the app may run in
/// the background, so a server started from a terminal died with the terminal.
/// Registered, the server is a launchd job of Hive's (xyz.matm.hive.server),
/// which `hive` starts on demand; it doesn't start at login, and launchd
/// restarts it only after a crash.
enum BackgroundServer {
    /// The job's plist in the bundle (Contents/Library/LaunchAgents), by bundle ID.
    static var plistName: String? {
        guard let id = Bundle.main.bundleIdentifier else { return nil }
        return "\(id).server.plist"
    }

    static var label: String? { Bundle.main.bundleIdentifier.map { "\($0).server" } }

    /// Only the machine's own server: not with a server, state or config of
    /// another (tests set these); the test bundle only when asked (debug).
    static var applies: Bool {
        let env = ProcessInfo.processInfo.environment
        #if DEBUG
        if env["HIVE_AGENT_TEST"] != nil { return true }
        #endif
        guard Bundle.main.bundleIdentifier == "xyz.matm.hive" else { return false }
        let others = ["HIVE_SERVER", "HIVE_STATE_DIR", "HIVE_CONFIG", "HIVE_SCRIPTS", "XDG_RUNTIME_DIR", "XDG_STATE_HOME", "XDG_CONFIG_HOME"]
        return !others.contains { !(env[$0] ?? "").isEmpty }
    }

    /// Registers the item, again whenever the app moved, its server was
    /// rebuilt or updated, or macOS stopped accepting the registration, so
    /// launchd runs this bundle's current server.
    /// A running server is never unregistered (that would stop it).
    static func register() {
        guard applies, let name = plistName, let label,
              FileManager.default.fileExists(atPath: Bundle.main.bundlePath + "/Contents/Library/LaunchAgents/" + name)
        else { return } // an older bundle without the item: started as before
        let service = SMAppService.agent(plistName: name)
        #if DEBUG
        // The e2e's cleanup: the test bundle's item out of Login Items.
        if ProcessInfo.processInfo.environment["HIVE_AGENT_UNREGISTER"] != nil {
            try? service.unregister()
            UserDefaults.standard.removeObject(forKey: "backgroundServerBuild")
            exit(0)
        }
        #endif
        // macOS binds the registration to the build it saw: after a rebuild or
        // an update the job fails to start until registered again.
        let server = Bundle.main.bundlePath + "/Contents/Resources/hive"
        let built = (try? FileManager.default.attributesOfItem(atPath: server)[.modificationDate] as? Date)?.timeIntervalSince1970 ?? 0
        let here = "\(Bundle.main.bundlePath)@\(built)"
        let key = "backgroundServerBuild"
        let defaults = UserDefaults.standard
        do {
            switch service.status {
            case .enabled where defaults.string(forKey: key) == here && !job(label).contains("spawn failed"):
                return
            case .enabled:
                if job(label).contains("\tpid = ") { return } // changed while it runs: next launch
                try service.unregister()
                try service.register()
            case .requiresApproval:
                NSLog("hive: allow Hive in Login Items & Extensions to keep terminals running when you close it")
                return
            default:
                try service.register()
            }
            defaults.set(here, forKey: key)
        } catch {
            NSLog("hive: background server: %@", String(describing: error))
        }
    }

    /// launchd's account of the job (`launchctl print`): "pid = " while it
    /// runs, "spawn failed" when macOS won't start this registration.
    static func job(_ label: String) -> String {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: "/bin/launchctl")
        p.arguments = ["print", "gui/\(getuid())/\(label)"]
        let out = Pipe()
        p.standardOutput = out
        p.standardError = FileHandle.nullDevice
        guard (try? p.run()) != nil else { return "" }
        let data = out.fileHandleForReading.readDataToEndOfFile()
        p.waitUntilExit()
        return String(decoding: data, as: UTF8.self)
    }
}
