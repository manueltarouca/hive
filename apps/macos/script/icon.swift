// Draws the Hive app icon at 1024×1024: the hexagon mark on a dark squircle.
// Usage: swift script/icon.swift out.png
import AppKit

let size: CGFloat = 1024
let out = CommandLine.arguments.dropFirst().first ?? "icon.png"

func hex(_ v: UInt32, _ a: CGFloat = 1) -> CGColor {
    CGColor(srgbRed: CGFloat((v >> 16) & 0xff) / 255, green: CGFloat((v >> 8) & 0xff) / 255, blue: CGFloat(v & 0xff) / 255, alpha: a)
}

/// A pointy-top hexagon with rounded corners.
func hexagon(center c: CGPoint, radius r: CGFloat, corner: CGFloat) -> CGPath {
    let pts = (0..<6).map { i -> CGPoint in
        let a = CGFloat.pi / 2 + CGFloat(i) * .pi / 3
        return CGPoint(x: c.x + r * cos(a), y: c.y + r * sin(a))
    }
    let p = CGMutablePath()
    let mid = { (a: CGPoint, b: CGPoint) in CGPoint(x: (a.x + b.x) / 2, y: (a.y + b.y) / 2) }
    p.move(to: mid(pts[5], pts[0]))
    for i in 0..<6 { p.addArc(tangent1End: pts[i], tangent2End: pts[(i + 1) % 6], radius: corner) }
    p.closeSubpath()
    return p
}

let space = CGColorSpace(name: CGColorSpace.sRGB)!
let ctx = CGContext(data: nil, width: Int(size), height: Int(size), bitsPerComponent: 8, bytesPerRow: 0,
                    space: space, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!

// macOS icon grid: an 824 pt squircle centred on the 1024 canvas, with a
// soft drop shadow below it.
let body = CGRect(x: 100, y: 100, width: 824, height: 824)
let squircle = CGPath(roundedRect: body, cornerWidth: 186, cornerHeight: 186, transform: nil)
ctx.saveGState()
ctx.setShadow(offset: CGSize(width: 0, height: -12), blur: 28, color: hex(0x000000, 0.35))
ctx.addPath(squircle); ctx.setFillColor(hex(0x1D1F21)); ctx.fillPath()
ctx.restoreGState()

// Background: Tomorrow Night, a little lighter at the top.
ctx.saveGState()
ctx.addPath(squircle); ctx.clip()
let bg = CGGradient(colorsSpace: space, colors: [hex(0x30343C), hex(0x1D1F21)] as CFArray, locations: [0, 1])!
ctx.drawLinearGradient(bg, start: CGPoint(x: 0, y: body.maxY), end: CGPoint(x: 0, y: body.minY), options: [])
// A warm glow behind the mark.
let glow = CGGradient(colorsSpace: space, colors: [hex(0xF2A93B, 0.22), hex(0xF2A93B, 0)] as CFArray, locations: [0, 1])!
ctx.drawRadialGradient(glow, startCenter: CGPoint(x: 512, y: 500), startRadius: 0,
                       endCenter: CGPoint(x: 512, y: 500), endRadius: 400, options: [])
ctx.restoreGState()

// A hairline on the squircle's top edge, like light catching glass.
ctx.saveGState()
ctx.addPath(squircle); ctx.clip()
ctx.addPath(CGPath(roundedRect: body.insetBy(dx: 1.5, dy: 1.5), cornerWidth: 184, cornerHeight: 184, transform: nil))
ctx.setStrokeColor(hex(0xFFFFFF, 0.10)); ctx.setLineWidth(3); ctx.strokePath()
ctx.restoreGState()

// The mark: a honey hexagon with a prompt cut into it.
let c = CGPoint(x: 512, y: 512)
ctx.saveGState()
ctx.addPath(hexagon(center: c, radius: 300, corner: 44)); ctx.clip()
let honey = CGGradient(colorsSpace: space, colors: [hex(0xF7BE5A), hex(0xE8922A)] as CFArray, locations: [0, 1])!
ctx.drawLinearGradient(honey, start: CGPoint(x: 0, y: 812), end: CGPoint(x: 0, y: 212), options: [])
ctx.restoreGState()

// "›_", centred optically: the chevron and the underscore share a baseline.
let ink = hex(0x24272D)
let chevron = CGMutablePath()
chevron.move(to: CGPoint(x: 398, y: 600))
chevron.addLine(to: CGPoint(x: 488, y: 512))
chevron.addLine(to: CGPoint(x: 398, y: 424))
ctx.addPath(chevron)
ctx.setStrokeColor(ink); ctx.setLineWidth(46); ctx.setLineCap(.round); ctx.setLineJoin(.round)
ctx.strokePath()
ctx.addPath(CGPath(roundedRect: CGRect(x: 526, y: 401, width: 122, height: 46), cornerWidth: 23, cornerHeight: 23, transform: nil))
ctx.setFillColor(ink); ctx.fillPath()

let img = ctx.makeImage()!
let rep = NSBitmapImageRep(cgImage: img)
try! rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: out))
