import Foundation

/// The code the phone scans (Hive menu, Share Hosts with Phone):
/// hive://hosts?d=BASE64URL({"hosts":[{"address","label"}],"v":1}). Labels
/// and addresses only, never secrets: who may use a server comes from the
/// tailnet. The phone treats it as untrusted and asks before saving.
public enum HostsCode {
    public static let prefix = "hive://hosts?d="

    public static func encode(_ hosts: [(label: String, address: String)]) -> String {
        struct Entry: Encodable { let label: String; let address: String }
        struct Payload: Encodable { let v: Int; let hosts: [Entry] }
        let e = JSONEncoder()
        e.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        let json = (try? e.encode(Payload(v: 1, hosts: hosts.map { Entry(label: $0.label, address: $0.address) }))) ?? Data()
        let b64 = json.base64EncodedString()
            .replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_")
            .trimmingCharacters(in: CharacterSet(charactersIn: "="))
        return prefix + b64
    }

    /// Where a phone on the tailnet reaches this Mac's server, from
    /// server.version: built-in Tailscale's own URL first, else a tailnet
    /// listener's address. nil when it listens on no tailnet.
    public static func thisMac(listeners: [(kind: String, addr: String)], tailscaleURLs: [String]) -> String? {
        if let u = tailscaleURLs.first { return u }
        guard let l = listeners.first(where: { $0.kind == "tailnet" }) else { return nil }
        return "http://" + l.addr
    }

    /// This Mac's label in the code: its own name (the hostname's first part,
    /// as a label allows), and "-mac" only if a saved host already has it.
    public static func label(forThisMac hostname: String, saved: [String]) -> String {
        let first = hostname.split(separator: ".").first.map(String.init) ?? ""
        var base = String(first.lowercased().map { $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" ? $0 : "-" })
        while base.hasPrefix("-") { base.removeFirst() }
        if base.isEmpty { base = "mac" }
        let taken = Set(saved.map { $0.lowercased() })
        guard taken.contains(base) else { return base }
        var label = base + "-mac", n = 2
        while taken.contains(label) { label = "\(base)-mac-\(n)"; n += 1 }
        return label
    }
}
