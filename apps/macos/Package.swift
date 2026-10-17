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
    products: [.executable(name: "Hive", targets: ["HiveApp"])],
    targets: [
        .executableTarget(name: "HiveApp", dependencies: ["HiveKit", "HiveCore", "HiveRender"],
                          swiftSettings: vtSwift + [.swiftLanguageMode(.v5)], linkerSettings: vtLink),
        .target(name: "CGhosttyVT", cSettings: vtC),
        // The protocol client: WebSocket over the unix socket, calls, events,
        // block streams. No libghostty, no AppKit.
        .target(name: "HiveKit", swiftSettings: [.swiftLanguageMode(.v5)]),
        .testTarget(name: "HiveKitTests", dependencies: ["HiveKit"],
                    swiftSettings: [.swiftLanguageMode(.v5)]),
        // Mirrors of blocks: libghostty terminals fed by the attach stream,
        // all confined to one serial queue, publishing immutable frames.
        .target(name: "HiveCore", dependencies: ["CGhosttyVT", "HiveKit"],
                swiftSettings: vtSwift + [.swiftLanguageMode(.v5)], linkerSettings: vtLink),
        .testTarget(name: "HiveCoreTests", dependencies: ["HiveCore", "HiveKit"],
                    swiftSettings: vtSwift + [.swiftLanguageMode(.v5)], linkerSettings: vtLink),
        // Metal renderer: glyph atlases, procedural box drawing, instanced
        // quads. Draws PaneFrames; never touches libghostty.
        .target(name: "HiveRender", dependencies: ["HiveCore"], resources: [.copy("Resources/fonts")],
                swiftSettings: vtSwift + [.swiftLanguageMode(.v5)]),
        .testTarget(name: "HiveRenderTests", dependencies: ["HiveRender", "HiveCore"],
                    resources: [.copy("Golden")],
                    swiftSettings: vtSwift + [.swiftLanguageMode(.v5)], linkerSettings: vtLink),
    ]
)
