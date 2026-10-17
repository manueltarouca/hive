import CoreGraphics
import Foundation
import ImageIO
import Testing
import UniformTypeIdentifiers
@testable import HiveCore
@testable import HiveRender

/// A screen that exercises the renderer.
func sampleFrame(cols: UInt16 = 60, rows: UInt16 = 12) throws -> PaneFrame {
    let m = try Mirror(cols: cols, rows: rows)
    m.apply(theme: .tomorrowNight)
    let lines = [
        "\u{1b}[1mhive\u{1b}[0m \u{1b}[2m· agents\u{1b}[0m  \u{1b}[32m● working\u{1b}[0m \u{1b}[33m◆ blocked\u{1b}[0m",
        "┌──────────┬─────────┐ ╔═══╗ ▁▂▃▄▅▆▇█ ░▒▓",
        "│ \u{1b}[34mclaude\u{1b}[0m   │ \u{1b}[3mitalic\u{1b}[0m  │ ║ok ║ ▀▄▌▐ ▖▗▘▝",
        "├──────────┼─────────┤ ╚═══╝",
        "└──────────┴─────────┘",
        "\u{1b}[38;2;240;198;116;48;2;55;59;65m truecolor \u{1b}[0m \u{1b}[4munder\u{1b}[0m \u{1b}[9mstrike\u{1b}[0m \u{1b}[7minverse\u{1b}[0m",
        "wide: 你好世界 emoji: 🙂🚀 nerd: \u{e0b0}\u{e0b1}\u{e0b2}\u{e0b3}",
        "\u{1b}[31mred \u{1b}[91mbright\u{1b}[0m \u{1b}[45m magenta bg \u{1b}[0m",
        "$ ",
    ]
    m.feed(Data(lines.joined(separator: "\r\n").utf8))
    return m.frame()
}

func png(_ img: CGImage, to url: URL) {
    let dest = CGImageDestinationCreateWithURL(url as CFURL, UTType.png.identifier as CFString, 1, nil)!
    CGImageDestinationAddImage(dest, img, nil)
    CGImageDestinationFinalize(dest)
}

func pixels(_ img: CGImage) -> [UInt8] {
    var buf = [UInt8](repeating: 0, count: img.width * img.height * 4)
    let ctx = CGContext(data: &buf, width: img.width, height: img.height, bitsPerComponent: 8, bytesPerRow: img.width * 4,
                        space: CGColorSpace(name: CGColorSpace.sRGB)!, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
    ctx.draw(img, in: CGRect(x: 0, y: 0, width: img.width, height: img.height))
    return buf
}

@Suite struct RendererTests {
    @Test func cellMetricsAreWholePixels() {
        let f = FontSet(pointSize: 13, scale: 2)
        #expect(f.cellWidth >= 14 && f.cellWidth <= 18)
        #expect(f.cellHeight >= 28 && f.cellHeight <= 40)
    }

    @Test func goldenSampleScreen() throws {
        let r = try Renderer(fonts: FontSet(pointSize: 13, scale: 2))
        let frame = try sampleFrame()
        let w = r.fonts.cellWidth * frame.cols + 24, h = r.fonts.cellHeight * frame.rows + 24
        let img = try #require(r.image([PaneDraw(frame: frame, origin: SIMD2(12, 12), size: SIMD2(Float(w - 24), Float(h - 24)), focused: true)],
                                       width: w, height: h))
        let out = URL(fileURLWithPath: "/tmp/hive-render-sample.png")
        png(img, to: out)

        let golden = URL(fileURLWithPath: #filePath).deletingLastPathComponent().appendingPathComponent("Golden/sample.png")
        guard FileManager.default.fileExists(atPath: golden.path), ProcessInfo.processInfo.environment["HIVE_RECORD"] == nil else {
            png(img, to: golden)
            Issue.record("recorded a new golden image at \(golden.path); check it and run again")
            return
        }
        let src = try #require(CGImageSourceCreateWithURL(golden as CFURL, nil))
        let want = try #require(CGImageSourceCreateImageAtIndex(src, 0, nil))
        #expect(want.width == img.width && want.height == img.height)
        let a = pixels(img), b = pixels(want)
        var differing = 0
        for i in stride(from: 0, to: min(a.count, b.count), by: 4) where abs(Int(a[i]) - Int(b[i])) > 24 || abs(Int(a[i + 1]) - Int(b[i + 1])) > 24 || abs(Int(a[i + 2]) - Int(b[i + 2])) > 24 {
            differing += 1
        }
        let fraction = Double(differing) / Double(a.count / 4)
        #expect(fraction < 0.002, "\(differing) pixels differ (\(fraction * 100)%); actual at \(out.path)")
    }

    @Test func spacersAndCursorInstances() throws {
        let r = try Renderer(fonts: FontSet(pointSize: 13, scale: 2))
        let m = try Mirror(cols: 10, rows: 2)
        m.feed(Data("你a".utf8))
        let f = m.frame()
        let inst = r.instances(for: [PaneDraw(frame: f, origin: .zero, size: SIMD2(200, 80), focused: true)], viewport: SIMD2(200, 80))
        let glyphs = inst.filter { $0.kind != 0 }
        #expect(glyphs.count == 2) // 你 once (its spacer skipped) and "a"
        #expect(glyphs.first?.size.x == Float(r.fonts.cellWidth * 2))
    }
}

@Suite struct LayoutTests {
    /// The shader reads instances with a 64-byte stride; Swift must agree.
    @Test func instanceStrideMatchesShader() {
        #expect(MemoryLayout<QuadInstance>.stride == 64)
        #expect(MemoryLayout<QuadInstance>.offset(of: \QuadInstance.color) == 32)
        #expect(MemoryLayout<QuadInstance>.offset(of: \QuadInstance.kind) == 48)
    }
}
