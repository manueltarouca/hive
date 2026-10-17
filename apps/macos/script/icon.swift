// Draws the Hive app icon at 1024×1024: three honeycomb cells, one lit, on a
// dark squircle. Many sessions; one needs you.
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
ctx.restoreGState()

// A hairline on the squircle's top edge, like light catching glass.
ctx.saveGState()
ctx.addPath(squircle); ctx.clip()
ctx.addPath(CGPath(roundedRect: body.insetBy(dx: 1.5, dy: 1.5), cornerWidth: 184, cornerHeight: 184, transform: nil))
ctx.setStrokeColor(hex(0xFFFFFF, 0.10)); ctx.setLineWidth(3); ctx.strokePath()
ctx.restoreGState()

// The mark: three pointy-top cells packed as in a honeycomb, the top one lit.
// Neighbouring centres are d apart, at 60° below the top cell. The group's
// bounding box is centred on the canvas.
let r: CGFloat = 150, gap: CGFloat = 24
let d = 3.squareRoot() * r + gap
let topY = 512 + d * 3.squareRoot() / 4
let lowY = topY - d * 3.squareRoot() / 2
let lit = CGPoint(x: 512, y: topY)
let cells = [CGPoint(x: 512 - d / 2, y: lowY), CGPoint(x: 512 + d / 2, y: lowY)]
let corner = r * 0.17

// A soft honey glow under the lit cell, inside the tile only.
ctx.saveGState()
ctx.addPath(squircle); ctx.clip()
let glow = CGGradient(colorsSpace: space, colors: [hex(0xF2A93B, 0.30), hex(0xF2A93B, 0)] as CFArray, locations: [0, 1])!
ctx.drawRadialGradient(glow, startCenter: lit, startRadius: 0, endCenter: lit, endRadius: r * 2.1, options: [])
ctx.restoreGState()

// Unlit cells: a step lighter than the tile, with a faint top edge.
for c in cells {
    ctx.saveGState()
    ctx.addPath(hexagon(center: c, radius: r, corner: corner)); ctx.clip()
    let slate = CGGradient(colorsSpace: space, colors: [hex(0x535963), hex(0x3D424A)] as CFArray, locations: [0, 1])!
    ctx.drawLinearGradient(slate, start: CGPoint(x: 0, y: c.y + r), end: CGPoint(x: 0, y: c.y - r), options: [])
    ctx.restoreGState()
    ctx.addPath(hexagon(center: c, radius: r - 1.5, corner: corner))
    ctx.setStrokeColor(hex(0xFFFFFF, 0.07)); ctx.setLineWidth(3); ctx.strokePath()
}

// The lit cell: honey, brighter at the top.
ctx.saveGState()
ctx.addPath(hexagon(center: lit, radius: r, corner: corner)); ctx.clip()
let honey = CGGradient(colorsSpace: space, colors: [hex(0xFAC66A), hex(0xE8922A)] as CFArray, locations: [0, 1])!
ctx.drawLinearGradient(honey, start: CGPoint(x: 0, y: lit.y + r), end: CGPoint(x: 0, y: lit.y - r), options: [])
ctx.restoreGState()
ctx.addPath(hexagon(center: lit, radius: r - 1.5, corner: corner))
ctx.setStrokeColor(hex(0xFFFFFF, 0.22)); ctx.setLineWidth(3); ctx.strokePath()

let img = ctx.makeImage()!
let rep = NSBitmapImageRep(cgImage: img)
try! rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: out))
