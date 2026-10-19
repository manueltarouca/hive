import AppKit
import CoreImage
import CoreImage.CIFilterBuiltins
import HiveKit
import SwiftUI

/// Hive → Share Hosts with Phone…: a code the phone scans (hive for
/// Android: Add a host, Scan the code) carrying the saved hosts, and this
/// Mac when its server listens on the tailnet. Labels and addresses only.
@MainActor
final class ShareHostsWindow {
    private var window: NSWindow?

    func show(model: AppModel) {
        let local = model.host(id: HostConnection.localID)
        let saved = model.hostsPath.map(AppModel.readHosts) ?? []
        let content = { (thisMac: String?, hostname: String) in
            ShareHostsView(saved: saved.map { ($0.label, $0.address) }, thisMac: thisMac.map { (model.newHostLabel(for: hostname), $0) })
        }
        let present = { (view: ShareHostsView) in
            if let w = self.window {
                w.contentView = NSHostingView(rootView: view)
                w.makeKeyAndOrderFront(nil)
                return
            }
            let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 420, height: 560),
                             styleMask: [.titled, .closable], backing: .buffered, defer: false)
            w.title = "Share Hosts with Phone"
            w.isReleasedWhenClosed = false
            w.contentView = NSHostingView(rootView: view)
            w.center()
            w.makeKeyAndOrderFront(nil)
            self.window = w
        }
        // Where the phone reaches this Mac comes from its server; without
        // an answer, the code has the saved hosts only.
        guard let local, local.connected else { present(content(nil, "")); return }
        local.call("server.version") { j in
            let listeners = j["listeners"].array.map { (kind: $0["kind"].string ?? "", addr: $0["addr"].string ?? "") }
            let urls = j["tailscale"]["urls"].array.compactMap(\.string)
            present(content(HostsCode.thisMac(listeners: listeners, tailscaleURLs: urls), j["hostname"].string ?? "mac"))
        }
    }
}

struct ShareHostsView: View {
    let saved: [(String, String)]
    let thisMac: (String, String)?
    @State private var copied = false

    private var hosts: [(label: String, address: String)] {
        (thisMac.map { [$0] } ?? []) + saved.filter { s in !(thisMac.map { $0.0.caseInsensitiveCompare(s.0) == .orderedSame } ?? false) }
    }

    private var code: String { HostsCode.encode(hosts) }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            if hosts.isEmpty {
                Text("No hosts to share yet").font(.headline)
                Text("Add a remote host first (Hive menu, Add Remote Host), or let the phone reach this Mac: start its server with")
                    .foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                Text("hive server start --extra-listen tailnet://:4483").font(.system(.callout, design: .monospaced)).textSelection(.enabled)
            } else {
                HStack {
                    Spacer()
                    if let image = QRImage.make(code) {
                        Image(nsImage: image).interpolation(.none).resizable().frame(width: 220, height: 220)
                            .padding(14).background(Color.white).clipShape(RoundedRectangle(cornerRadius: 12))
                            .accessibilityLabel("Hosts code")
                    } else {
                        // About 20 hosts fill a code: the copied code has no such limit.
                        Text("Too many hosts for one code. Copy Code, and on the phone use Paste it.")
                            .foregroundStyle(.secondary).frame(width: 248, height: 248)
                            .background(RoundedRectangle(cornerRadius: 12).strokeBorder(.quaternary))
                    }
                    Spacer()
                }
                Text("On your phone, in hive: Add a host, then Scan the code.")
                    .fixedSize(horizontal: false, vertical: true)
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(Array(hosts.enumerated()), id: \.offset) { _, h in
                        VStack(alignment: .leading, spacing: 1) {
                            Text(h.label)
                            Text(h.address).font(.callout).foregroundStyle(.secondary).lineLimit(1)
                        }
                    }
                }
                if thisMac == nil {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("This Mac isn't in the code: its server doesn't listen on your tailnet. To reach it from the phone, start it with")
                            .font(.callout).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                        Text("hive server start --extra-listen tailnet://:4483").font(.system(.callout, design: .monospaced)).textSelection(.enabled)
                    }
                }
                HStack {
                    Text("Labels and addresses only: who may connect comes from your tailnet.")
                        .font(.callout).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                    Spacer()
                    Button(copied ? "Copied" : "Copy Code") {
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(code, forType: .string)
                        copied = true
                    }
                }
            }
        }
        .padding(24)
        .frame(width: 420)
    }
}

enum QRImage {
    /// A crisp QR image: one point per module, scaled up by the view without smoothing.
    static func make(_ text: String) -> NSImage? {
        let f = CIFilter.qrCodeGenerator()
        f.message = Data(text.utf8)
        f.correctionLevel = "M"
        guard let ci = f.outputImage else { return nil }
        let rep = NSCIImageRep(ciImage: ci)
        let image = NSImage(size: rep.size)
        image.addRepresentation(rep)
        return image
    }
}
