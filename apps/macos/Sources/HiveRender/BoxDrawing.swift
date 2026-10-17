import CoreGraphics

/// Box-drawing, block and Powerline characters drawn as shapes rather than
/// font glyphs, so they fill their cells exactly and TUI frames join without
/// gaps, whatever the font.
enum BoxDrawing {
    static func handles(_ u: Unicode.Scalar) -> Bool {
        switch u.value {
        case 0x2500...0x257F: return lines[u.value] != nil
        case 0x2580...0x259F, 0xE0B0...0xE0B3: return true
        default: return false
        }
    }

    /// Line weights toward (up, right, down, left): 0 none, 1 light, 2 heavy, 3 double.
    private static let lines: [UInt32: (UInt8, UInt8, UInt8, UInt8)] = {
        var t: [UInt32: (UInt8, UInt8, UInt8, UInt8)] = [
            0x2500: (0, 1, 0, 1), 0x2501: (0, 2, 0, 2), 0x2502: (1, 0, 1, 0), 0x2503: (2, 0, 2, 0),
            0x250C: (0, 1, 1, 0), 0x250F: (0, 2, 2, 0), 0x2510: (0, 0, 1, 1), 0x2513: (0, 0, 2, 2),
            0x2514: (1, 1, 0, 0), 0x2517: (2, 2, 0, 0), 0x2518: (1, 0, 0, 1), 0x251B: (2, 0, 0, 2),
            0x251C: (1, 1, 1, 0), 0x2523: (2, 2, 2, 0), 0x2524: (1, 0, 1, 1), 0x252B: (2, 0, 2, 2),
            0x252C: (0, 1, 1, 1), 0x2533: (0, 2, 2, 2), 0x2534: (1, 1, 0, 1), 0x253B: (2, 2, 0, 2),
            0x253C: (1, 1, 1, 1), 0x254B: (2, 2, 2, 2),
            0x2550: (0, 3, 0, 3), 0x2551: (3, 0, 3, 0), 0x2554: (0, 3, 3, 0), 0x2557: (0, 0, 3, 3),
            0x255A: (3, 3, 0, 0), 0x255D: (3, 0, 0, 3), 0x2560: (3, 3, 3, 0), 0x2563: (3, 0, 3, 3),
            0x2566: (0, 3, 3, 3), 0x2569: (3, 3, 0, 3), 0x256C: (3, 3, 3, 3),
            // Rounded corners, drawn square at this size.
            0x256D: (0, 1, 1, 0), 0x256E: (0, 0, 1, 1), 0x256F: (1, 0, 0, 1), 0x2570: (1, 1, 0, 0),
            0x2574: (0, 0, 0, 1), 0x2575: (1, 0, 0, 0), 0x2576: (0, 1, 0, 0), 0x2577: (0, 0, 1, 0),
            0x2578: (0, 0, 0, 2), 0x2579: (2, 0, 0, 0), 0x257A: (0, 2, 0, 0), 0x257B: (0, 0, 2, 0),
        ]
        // Dashed lines draw solid.
        for (u, v) in [(0x2504, 0x2500), (0x2505, 0x2501), (0x2506, 0x2502), (0x2507, 0x2503),
                       (0x2508, 0x2500), (0x2509, 0x2501), (0x250A, 0x2502), (0x250B, 0x2503),
                       (0x254C, 0x2500), (0x254D, 0x2501), (0x254E, 0x2502), (0x254F, 0x2503)] {
            t[UInt32(u)] = t[UInt32(v)]
        }
        return t
    }()

    static func draw(_ u: Unicode.Scalar, in ctx: CGContext, width w: CGFloat, height h: CGFloat, thickness: CGFloat) {
        ctx.setFillColor(CGColor(gray: 1, alpha: 1))
        let v = u.value
        if let l = lines[v] { drawLines(l, ctx, w, h, thickness); return }
        switch v {
        case 0x2580: ctx.fill(CGRect(x: 0, y: h / 2, width: w, height: h / 2))           // ▀
        case 0x2581...0x2588: ctx.fill(CGRect(x: 0, y: 0, width: w, height: (h * CGFloat(v - 0x2580) / 8).rounded())) // ▁…█
        case 0x2589...0x258F: ctx.fill(CGRect(x: 0, y: 0, width: (w * CGFloat(0x2590 - v) / 8).rounded(), height: h)) // ▉…▏
        case 0x2590: ctx.fill(CGRect(x: w / 2, y: 0, width: w / 2, height: h))           // ▐
        case 0x2591, 0x2592, 0x2593:                                                     // ░▒▓
            ctx.setFillColor(CGColor(gray: 1, alpha: [0.25, 0.5, 0.75][Int(v - 0x2591)]))
            ctx.fill(CGRect(x: 0, y: 0, width: w, height: h))
        case 0x2594: ctx.fill(CGRect(x: 0, y: h - (h / 8).rounded(), width: w, height: (h / 8).rounded())) // ▔
        case 0x2595: ctx.fill(CGRect(x: w - (w / 8).rounded(), y: 0, width: (w / 8).rounded(), height: h)) // ▕
        case 0x2596...0x259F:
            // Quadrants: upper-left, upper-right, lower-left, lower-right bits.
            let q: [UInt32: UInt8] = [0x2596: 0b0010, 0x2597: 0b0001, 0x2598: 0b1000, 0x2599: 0b1011,
                                      0x259A: 0b1001, 0x259B: 0b1110, 0x259C: 0b1101, 0x259D: 0b0100,
                                      0x259E: 0b0110, 0x259F: 0b0111]
            let bits = q[v] ?? 0
            if bits & 0b1000 != 0 { ctx.fill(CGRect(x: 0, y: h / 2, width: w / 2, height: h / 2)) }
            if bits & 0b0100 != 0 { ctx.fill(CGRect(x: w / 2, y: h / 2, width: w / 2, height: h / 2)) }
            if bits & 0b0010 != 0 { ctx.fill(CGRect(x: 0, y: 0, width: w / 2, height: h / 2)) }
            if bits & 0b0001 != 0 { ctx.fill(CGRect(x: w / 2, y: 0, width: w / 2, height: h / 2)) }
        case 0xE0B0, 0xE0B2: // Powerline solid triangles
            let right = v == 0xE0B0
            ctx.beginPath()
            ctx.move(to: CGPoint(x: right ? 0 : w, y: 0))
            ctx.addLine(to: CGPoint(x: right ? w : 0, y: h / 2))
            ctx.addLine(to: CGPoint(x: right ? 0 : w, y: h))
            ctx.closePath()
            ctx.fillPath()
        case 0xE0B1, 0xE0B3: // Powerline thin arrows
            let right = v == 0xE0B1
            ctx.setStrokeColor(CGColor(gray: 1, alpha: 1))
            ctx.setLineWidth(thickness)
            ctx.beginPath()
            ctx.move(to: CGPoint(x: right ? 0 : w, y: 0))
            ctx.addLine(to: CGPoint(x: right ? w : 0, y: h / 2))
            ctx.addLine(to: CGPoint(x: right ? 0 : w, y: h))
            ctx.strokePath()
        default: break
        }
    }

    private static func drawLines(_ l: (UInt8, UInt8, UInt8, UInt8), _ ctx: CGContext, _ w: CGFloat, _ h: CGFloat, _ t: CGFloat) {
        let cx = (w / 2).rounded(.down), cy = (h / 2).rounded(.down)
        func thick(_ weight: UInt8) -> CGFloat { weight == 2 ? t * 2 : t }
        // Each arm runs from the cell edge to the center, overlapping by
        // half a line so arms of different weights still meet.
        func arm(_ weight: UInt8, horizontal: Bool, toLow: Bool) {
            guard weight > 0 else { return }
            let offsets: [CGFloat] = weight == 3 ? [-t, t] : [0]
            for o in offsets {
                let k = thick(weight)
                if horizontal {
                    let y = cy + o - k / 2
                    let reach = cx + (weight == 3 ? t : k / 2)
                    ctx.fill(toLow ? CGRect(x: 0, y: y, width: reach, height: k)
                                   : CGRect(x: cx - (weight == 3 ? t : k / 2), y: y, width: w - cx + (weight == 3 ? t : k / 2), height: k))
                } else {
                    let x = cx + o - k / 2
                    let reach = cy + (weight == 3 ? t : k / 2)
                    ctx.fill(toLow ? CGRect(x: x, y: 0, width: k, height: reach)
                                   : CGRect(x: x, y: cy - (weight == 3 ? t : k / 2), width: k, height: h - cy + (weight == 3 ? t : k / 2)))
                }
            }
        }
        // CoreGraphics' y grows upward: "up" is the high side.
        arm(l.0, horizontal: false, toLow: false)
        arm(l.1, horizontal: true, toLow: false)
        arm(l.2, horizontal: false, toLow: true)
        arm(l.3, horizontal: true, toLow: true)
    }
}
