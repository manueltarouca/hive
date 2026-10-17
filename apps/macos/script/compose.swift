// Composes a window capture (with its shadow) onto hive's backdrop, for the
// website. Usage: swift compose.swift in.png out.png [light|dark] [width]
import AppKit
import CoreGraphics
import ImageIO
import UniformTypeIdentifiers

let args = CommandLine.arguments
let input = URL(fileURLWithPath: args[1]), output = URL(fileURLWithPath: args[2])
let dark = args.count > 3 && args[3] == "dark"
let outWidth = args.count > 4 ? Int(args[4])! : 2400

guard let src = CGImageSourceCreateWithURL(input as CFURL, nil), let shot = CGImageSourceCreateImageAtIndex(src, 0, nil) else {
    fatalError("can't read \(input.path)")
}
// The backdrop: a soft vertical wash in hive's colors, a little wider than the window.
let pad = Double(shot.width) * 0.075
let w = Double(shot.width) + pad * 2, h = Double(shot.height) + pad * 1.6
let space = CGColorSpace(name: CGColorSpace.sRGB)!
let ctx = CGContext(data: nil, width: Int(w), height: Int(h), bitsPerComponent: 8, bytesPerRow: 0, space: space,
                    bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
func c(_ hex: UInt32, _ a: CGFloat = 1) -> CGColor {
    CGColor(srgbRed: CGFloat((hex >> 16) & 0xff) / 255, green: CGFloat((hex >> 8) & 0xff) / 255, blue: CGFloat(hex & 0xff) / 255, alpha: a)
}
let stops: [CGColor] = dark ? [c(0x2A2318), c(0x17151A), c(0x121116)] : [c(0xF7D9A0), c(0xF4E7D2), c(0xF2EEE6)]
let grad = CGGradient(colorsSpace: space, colors: stops as CFArray, locations: [0, 0.55, 1])!
// Paint past both ends, or the corners beyond them stay empty.
ctx.drawLinearGradient(grad, start: CGPoint(x: 0, y: h), end: CGPoint(x: w * 0.35, y: 0),
                       options: [.drawsBeforeStartLocation, .drawsAfterEndLocation])
// Centered: the capture's own shadow already weights it downward.
ctx.draw(shot, in: CGRect(x: (w - Double(shot.width)) / 2, y: (h - Double(shot.height)) / 2,
                          width: Double(shot.width), height: Double(shot.height)))
let full = ctx.makeImage()!

// Scale to the output width.
let outH = Int(Double(outWidth) * h / w)
let out = CGContext(data: nil, width: outWidth, height: outH, bitsPerComponent: 8, bytesPerRow: 0, space: space,
                    bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
out.interpolationQuality = .high
out.draw(full, in: CGRect(x: 0, y: 0, width: outWidth, height: outH))
let dest = CGImageDestinationCreateWithURL(output as CFURL, UTType.png.identifier as CFString, 1, nil)!
CGImageDestinationAddImage(dest, out.makeImage()!, nil)
CGImageDestinationFinalize(dest)
print(output.path, outWidth, "x", outH)
