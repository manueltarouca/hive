// swift-tools-version:6.0
import PackageDescription
import Foundation

// libghostty-vt is built by `make vt` at the repository root.
let root = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
    .deletingLastPathComponent().deletingLastPathComponent().path
let vt = root + "/third_party/ghostty-vt"
let vtC: [CSetting] = [.unsafeFlags(["-I\(vt)/include", "-DGHOSTTY_STATIC"])]
let vtSwift: [SwiftSetting] = [.unsafeFlags(["-Xcc", "-I\(vt)/include", "-Xcc", "-DGHOSTTY_STATIC"])]
let vtLink: [LinkerSetting] = [.unsafeFlags(["\(vt)/lib/libghostty-vt.a"])]

let package = Package(
    name: "Hive",
    platforms: [.macOS(.v14)],
    targets: [
        .target(name: "CGhosttyVT", cSettings: vtC),
        // The protocol client: WebSocket over the unix socket, calls, events,
        // block streams. No libghostty, no AppKit.
        .target(name: "HiveKit", swiftSettings: [.swiftLanguageMode(.v5)]),
        .testTarget(name: "HiveKitTests", dependencies: ["HiveKit"],
                    swiftSettings: [.swiftLanguageMode(.v5)]),
    ]
)
