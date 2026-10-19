import Foundation
import HiveKit

/// A saved remote host, as hosts.json has it.
struct SavedHost: Equatable, Decodable {
    let label: String
    let address: String
    /// "always", or on demand (missing, or "on-demand").
    var connect: String? = nil
    var always: Bool { connect == "always" }
}

extension AppModel {
    /// hosts.json, beside the settings file: the CLI's `hive hosts` and the
    /// app share it. Written only through the bundled hive, which validates
    /// and locks it.
    var hostsPath: String? {
        let p = settingsStore.settings.path
        guard !p.isEmpty else { return nil }
        return ((p as NSString).deletingLastPathComponent as NSString).appendingPathComponent("hosts.json")
    }

    static func readHosts(_ path: String) -> [SavedHost] {
        struct File: Decodable { let hosts: [SavedHost] }
        guard let d = FileManager.default.contents(atPath: path) else { return [] }
        return (try? JSONDecoder().decode(File.self, from: d))?.hosts ?? []
    }

    /// Reads hosts.json now and every 2 s after, connecting to hosts that
    /// appear and closing those that go.
    func watchHosts() {
        loadHosts()
        hostsTimer?.invalidate()
        hostsTimer = Timer.scheduledTimer(withTimeInterval: 2, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.loadHosts() }
        }
    }

    func loadHosts() {
        guard let path = hostsPath else { return }
        let stamp = (try? FileManager.default.attributesOfItem(atPath: path))?[.modificationDate] as? Date
        guard stamp != hostsStamp || path != hostsStampPath else { return }
        hostsStamp = stamp
        hostsStampPath = path
        syncHosts(AppModel.readHosts(path))
    }

    /// Makes the connections match the saved hosts: this Mac's always first.
    func syncHosts(_ saved: [SavedHost]) {
        var next: [HostConnection] = hosts.filter(\.isLocal)
        for s in saved {
            let eps = HostConnection.endpoints(for: s.address)
            let id = HostConnection.remoteID(s.label)
            if let h = hosts.first(where: { $0.id == id }) {
                var keep = h.endpoints == eps
                // Saved as the URL it already answered at: keep the connection.
                if !keep, eps.count == 1, h.endpoint == eps[0] { h.pin(to: eps[0]); keep = true }
                if keep {
                    if s.always, !h.savedAlways { h.start() } // switched to always: connect now
                    h.savedAlways = s.always
                    next.append(h)
                    continue
                }
            }
            // A new address for a host in use stays in use.
            let wasActive = hosts.first { $0.id == id }?.active ?? false
            guard !eps.isEmpty else { continue }
            let h = HostConnection(id: id, label: s.label, endpoints: eps, vt: vt)
            h.theme = hosts[0].theme
            h.cursorBlink = hosts[0].cursorBlink
            wireHost(h)
            h.onPinned = { [weak self] h, e in self?.saveAnswered(h, e) }
            // On demand: listed, connected when picked; or at launch when the
            // session you were last in is on it.
            h.savedAlways = s.always
            let last = AppModel.defaults.string(forKey: "lastSessionHost")
            if s.always || wasActive || last?.caseInsensitiveCompare(s.label) == .orderedSame { h.start() }
            next.append(h)
        }
        for h in hosts where !next.contains(where: { $0 === h }) {
            h.close()
            pendingConnected[h.id] = nil
            if h === current, let local = next.first { select(onHost: local) }
        }
        setHosts(next)
    }

    /// Adds a host through the bundled hive (which checks the address and
    /// saves the URL it answers at). Answers nil, or what went wrong.
    func addHost(label: String, address: String, then: @escaping @MainActor (String?) -> Void) {
        DispatchQueue.global().async {
            let r = SettingsStore.run(["hosts", "add", label, address])
            let err: String? = r == nil ? "hive isn't installed with the app"
                : r!.status == 0 ? nil : r!.err.replacingOccurrences(of: "Error: ", with: "").trimmingCharacters(in: .whitespacesAndNewlines)
            Task { @MainActor in
                self.hostsStamp = nil
                self.loadHosts()
                then(err)
            }
        }
    }

    /// A bare host answered at one URL: save that, as `hive hosts add`
    /// does, so it isn't probed again (nor downgraded to plain HTTP).
    func saveAnswered(_ h: HostConnection, _ e: ServerEndpoint) {
        let label = h.label, url = e.description
        DispatchQueue.global().async { _ = SettingsStore.run(["hosts", "add", label, url]) }
    }

    /// A label for a host added by its address, as `hive hosts` wants one:
    /// its first name (box, for box.example.ts.net), made unique.
    func newHostLabel(for address: String) -> String {
        var host = address
        if let r = host.range(of: "://") { host = String(host[r.upperBound...]) }
        host = String(host.split(separator: "/").first ?? "")
        if host.hasPrefix("[") { host = String(host.dropFirst().prefix { $0 != "]" }) }
        else if host.filter({ $0 == ":" }).count == 1 { host = String(host.prefix { $0 != ":" }) }
        let isIP = host.allSatisfy { $0.isHexDigit || $0 == "." || $0 == ":" }
        var base = isIP ? host : String(host.split(separator: ".").first ?? "")
        base = String(base.map { $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" ? $0 : "-" })
        if base.isEmpty { base = "host" }
        let taken = Set(hosts.map { $0.label.lowercased() })
        var label = base, n = 2
        while taken.contains(label.lowercased()) { label = "\(base)-\(n)"; n += 1 }
        return label
    }

    /// Connects an on-demand host; `then` runs once it's connected.
    func connect(host id: String, then: (() -> Void)? = nil) {
        guard let h = host(id: id) else { return }
        h.start()
        publishHosts()
        if let then { whenConnected(h, then: then) }
    }

    /// Lets a remote host go. Its sessions leave the list until it's
    /// picked again; if one was on screen, this Mac's takes its place.
    func disconnect(host id: String) {
        guard let h = host(id: id), !h.isLocal else { return }
        let wasCurrent = h === current
        h.stop()
        pendingConnected[h.id] = nil
        if wasCurrent, let local = hosts.first(where: \.isLocal) { select(onHost: local) }
        publishHosts()
    }

    func removeHost(_ label: String) {
        DispatchQueue.global().async {
            _ = SettingsStore.run(["hosts", "rm", label])
            Task { @MainActor in
                self.hostsStamp = nil
                self.loadHosts()
            }
        }
    }
}
