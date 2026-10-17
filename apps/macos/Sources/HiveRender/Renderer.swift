import CoreGraphics
import Foundation
import HiveCore
import Metal
import simd

/// A pane to draw: its frame, where it goes, and how.
public struct PaneDraw {
    public var frame: PaneFrame
    /// Top-left corner in pixels.
    public var origin: SIMD2<Float>
    public var focused: Bool
    /// Pixels of the pane area beyond the grid (letterbox), filled with the
    /// background.
    public var size: SIMD2<Float>
    public init(frame: PaneFrame, origin: SIMD2<Float>, size: SIMD2<Float>, focused: Bool) {
        self.frame = frame; self.origin = origin; self.size = size; self.focused = focused
    }
}

struct QuadInstance {
    var pos: SIMD2<Float>   // top-left, pixels
    var size: SIMD2<Float>
    var uv: SIMD2<Float>    // atlas top-left, pixels
    var color: SIMD4<Float>
    var kind: UInt32        // 0 solid, 1 gray glyph, 2 color glyph
    var pad: (UInt32, UInt32, UInt32) = (0, 0, 0)
}

struct Uniforms {
    var viewport: SIMD2<Float>
    var atlasSize: SIMD2<Float>
}

/// Draws panes with Metal. Glyphs come from a cell-sized tile atlas; every
/// cell is at most a background quad, a glyph quad and a decoration quad.
public final class Renderer {
    public let device: MTLDevice
    public let fonts: FontSet
    let queue: MTLCommandQueue
    let pipeline: MTLRenderPipelineState
    let atlas: Atlas
    public var theme: Theme
    public var pixelFormat: MTLPixelFormat

    // Triple-buffered instance storage.
    private var buffers: [MTLBuffer] = []
    private var bufferIndex = 0
    private let inflight = DispatchSemaphore(value: 3)

    public init(device: MTLDevice? = nil, fonts: FontSet = FontSet(), theme: Theme = .tomorrowNight,
                pixelFormat: MTLPixelFormat = .bgra8Unorm) throws {
        guard let device = device ?? MTLCreateSystemDefaultDevice(), let queue = device.makeCommandQueue() else {
            throw NSError(domain: "hive.render", code: 1, userInfo: [NSLocalizedDescriptionKey: "no Metal device"])
        }
        self.device = device
        self.queue = queue
        self.fonts = fonts
        self.theme = theme
        self.pixelFormat = pixelFormat
        atlas = Atlas(device: device, fonts: fonts)
        // Compiled at run time: the Command Line Tools have no metal compiler.
        let library = try device.makeLibrary(source: Renderer.shaders, options: nil)
        let d = MTLRenderPipelineDescriptor()
        d.vertexFunction = library.makeFunction(name: "quad_vertex")
        d.fragmentFunction = library.makeFunction(name: "quad_fragment")
        let ca = d.colorAttachments[0]!
        ca.pixelFormat = pixelFormat
        ca.isBlendingEnabled = true
        ca.rgbBlendOperation = .add
        ca.alphaBlendOperation = .add
        ca.sourceRGBBlendFactor = .one
        ca.sourceAlphaBlendFactor = .one
        ca.destinationRGBBlendFactor = .oneMinusSourceAlpha
        ca.destinationAlphaBlendFactor = .oneMinusSourceAlpha
        pipeline = try device.makeRenderPipelineState(descriptor: d)
    }

    // MARK: building instances

    private func color(_ c: RGB, alpha: Float = 1) -> SIMD4<Float> {
        SIMD4(Float(c.r) / 255 * alpha, Float(c.g) / 255 * alpha, Float(c.b) / 255 * alpha, alpha)
    }

    /// Instances for the panes: backgrounds first, then glyphs, then
    /// decorations and the cursor.
    func instances(for panes: [PaneDraw], viewport: SIMD2<Float>) -> [QuadInstance] {
        var bg: [QuadInstance] = [], fg: [QuadInstance] = [], deco: [QuadInstance] = []
        let cw = Float(fonts.cellWidth), ch = Float(fonts.cellHeight)
        for p in panes {
            let f = p.frame
            let defaultBG = f.background, defaultFG = f.foreground
            bg.append(QuadInstance(pos: p.origin, size: p.size, uv: .zero, color: color(defaultBG), kind: 0))
            for (y, row) in f.cells.enumerated() {
                for (x, cell) in row.enumerated() {
                    if cell.wide == .spacerTail || cell.wide == .spacerHead { continue }
                    let pos = p.origin + SIMD2(Float(x) * cw, Float(y) * ch)
                    let width: Float = cell.wide == .wide ? 2 : 1
                    var fgc = cell.fg ?? defaultFG, bgc = cell.bg
                    if cell.inverse { (fgc, bgc) = (bgc ?? defaultBG, fgc) }
                    if cell.selected { bgc = theme.selection }
                    let isCursor = p.focused && f.cursor.visible && f.cursor.style == .block && f.cursor.x == x && f.cursor.y == y
                    if isCursor { bgc = theme.cursor; fgc = defaultBG }
                    if let bgc { bg.append(QuadInstance(pos: pos, size: SIMD2(cw * width, ch), uv: .zero, color: color(bgc), kind: 0)) }
                    if cell.invisible || cell.text.isEmpty || cell.text == " " { continue }
                    let key = GlyphKey(text: cell.text, bold: cell.bold, italic: cell.italic, wide: cell.wide == .wide)
                    guard let slot = atlas.slot(key) else { continue }
                    let alpha: Float = cell.faint ? 0.6 : 1
                    fg.append(QuadInstance(pos: pos, size: SIMD2(Float(slot.w), Float(slot.h)),
                                           uv: SIMD2(Float(slot.x), Float(slot.y)),
                                           color: slot.color ? SIMD4(alpha, alpha, alpha, alpha) : color(fgc, alpha: alpha),
                                           kind: slot.color ? 2 : 1))
                    let thick = Float(fonts.underlineThickness)
                    if cell.underline != 0 {
                        deco.append(QuadInstance(pos: pos + SIMD2(0, ch - Float(fonts.underlinePosition) - thick / 2),
                                                 size: SIMD2(cw * width, thick), uv: .zero, color: color(fgc, alpha: alpha), kind: 0))
                    }
                    if cell.strikethrough {
                        deco.append(QuadInstance(pos: pos + SIMD2(0, (ch / 2).rounded()), size: SIMD2(cw * width, thick),
                                                 uv: .zero, color: color(fgc, alpha: alpha), kind: 0))
                    }
                }
            }
            // Non-block cursors, and a hollow block for unfocused panes.
            if f.cursor.visible, f.cursor.y < f.rows {
                let pos = p.origin + SIMD2(Float(f.cursor.x) * cw, Float(f.cursor.y) * ch)
                let c = color(theme.cursor)
                let t = max(1, Float(fonts.scale).rounded())
                switch (p.focused, f.cursor.style) {
                case (true, .bar): deco.append(QuadInstance(pos: pos, size: SIMD2(2 * t, ch), uv: .zero, color: c, kind: 0))
                case (true, .underline): deco.append(QuadInstance(pos: pos + SIMD2(0, ch - 2 * t), size: SIMD2(cw, 2 * t), uv: .zero, color: c, kind: 0))
                case (true, .block): break
                default:
                    for r in [(SIMD2<Float>(0, 0), SIMD2(cw, t)), (SIMD2(0, ch - t), SIMD2(cw, t)),
                              (SIMD2(0, 0), SIMD2(t, ch)), (SIMD2(cw - t, 0), SIMD2(t, ch))] {
                        deco.append(QuadInstance(pos: pos + r.0, size: r.1, uv: .zero, color: c, kind: 0))
                    }
                }
            }
        }
        return bg + fg + deco
    }

    // MARK: drawing

    /// Encodes the panes into a render pass on `texture`.
    public func encode(_ panes: [PaneDraw], into texture: MTLTexture, commandBuffer: MTLCommandBuffer, clear: RGB? = nil) {
        let viewport = SIMD2(Float(texture.width), Float(texture.height))
        let inst = instances(for: panes, viewport: viewport)
        let rp = MTLRenderPassDescriptor()
        rp.colorAttachments[0].texture = texture
        rp.colorAttachments[0].loadAction = .clear
        let cc = clear ?? theme.background
        rp.colorAttachments[0].clearColor = MTLClearColor(red: Double(cc.r) / 255, green: Double(cc.g) / 255, blue: Double(cc.b) / 255, alpha: 1)
        rp.colorAttachments[0].storeAction = .store
        guard let enc = commandBuffer.makeRenderCommandEncoder(descriptor: rp) else { return }
        defer { enc.endEncoding() }
        guard !inst.isEmpty else { return }

        inflight.wait()
        commandBuffer.addCompletedHandler { [inflight] _ in inflight.signal() }
        let bytes = inst.count * MemoryLayout<QuadInstance>.stride
        if buffers.count < 3 { buffers.append(device.makeBuffer(length: max(bytes, 1 << 16), options: .storageModeShared)!) }
        bufferIndex = (bufferIndex + 1) % buffers.count
        if buffers[bufferIndex].length < bytes {
            buffers[bufferIndex] = device.makeBuffer(length: bytes * 2, options: .storageModeShared)!
        }
        let buf = buffers[bufferIndex]
        inst.withUnsafeBytes { buf.contents().copyMemory(from: $0.baseAddress!, byteCount: bytes) }

        var u = Uniforms(viewport: viewport, atlasSize: SIMD2(Float(atlas.size), Float(atlas.size)))
        enc.setRenderPipelineState(pipeline)
        enc.setVertexBuffer(buf, offset: 0, index: 0)
        enc.setVertexBytes(&u, length: MemoryLayout<Uniforms>.stride, index: 1)
        enc.setFragmentTexture(atlas.gray, index: 0)
        enc.setFragmentTexture(atlas.color, index: 1)
        enc.drawPrimitives(type: .triangle, vertexStart: 0, vertexCount: 6, instanceCount: inst.count)
    }

    /// Renders offscreen and returns an image, for tests and screenshots.
    public func image(_ panes: [PaneDraw], width: Int, height: Int) -> CGImage? {
        let d = MTLTextureDescriptor.texture2DDescriptor(pixelFormat: pixelFormat, width: width, height: height, mipmapped: false)
        d.usage = [.renderTarget, .shaderRead]
        d.storageMode = .shared
        guard let tex = device.makeTexture(descriptor: d), let cb = queue.makeCommandBuffer() else { return nil }
        encode(panes, into: tex, commandBuffer: cb)
        cb.commit()
        cb.waitUntilCompleted()
        var pixels = [UInt8](repeating: 0, count: width * height * 4)
        tex.getBytes(&pixels, bytesPerRow: width * 4, from: MTLRegionMake2D(0, 0, width, height), mipmapLevel: 0)
        guard let provider = CGDataProvider(data: Data(pixels) as CFData) else { return nil }
        return CGImage(width: width, height: height, bitsPerComponent: 8, bitsPerPixel: 32, bytesPerRow: width * 4,
                       space: CGColorSpace(name: CGColorSpace.sRGB)!,
                       bitmapInfo: CGBitmapInfo(rawValue: CGImageAlphaInfo.noneSkipFirst.rawValue | CGBitmapInfo.byteOrder32Little.rawValue),
                       provider: provider, decode: nil, shouldInterpolate: false, intent: .defaultIntent)
    }

    public func makeCommandBuffer() -> MTLCommandBuffer? { queue.makeCommandBuffer() }

    static let shaders = """
    #include <metal_stdlib>
    using namespace metal;

    struct Quad {
        float2 pos; float2 size; float2 uv; float4 color; uint kind; uint pad0, pad1, pad2; // 64 bytes, as in Swift (uint3 would be 16-aligned)
    };
    struct Uniforms { float2 viewport; float2 atlasSize; };
    struct VOut { float4 position [[position]]; float2 uv; float4 color; uint kind [[flat]]; };

    vertex VOut quad_vertex(uint vid [[vertex_id]], uint iid [[instance_id]],
                            const device Quad* quads [[buffer(0)]], constant Uniforms& u [[buffer(1)]]) {
        const float2 corners[6] = { {0,0}, {1,0}, {0,1}, {1,0}, {1,1}, {0,1} };
        Quad q = quads[iid];
        float2 c = corners[vid];
        float2 p = q.pos + c * q.size;
        VOut o;
        o.position = float4(p.x / u.viewport.x * 2 - 1, 1 - p.y / u.viewport.y * 2, 0, 1);
        o.uv = (q.uv + c * q.size) / u.atlasSize;
        o.color = q.color;
        o.kind = q.kind;
        return o;
    }

    fragment float4 quad_fragment(VOut in [[stage_in]],
                                  texture2d<float> gray [[texture(0)]], texture2d<float> color [[texture(1)]]) {
        constexpr sampler s(coord::normalized, filter::nearest);
        if (in.kind == 0) return in.color;
        if (in.kind == 1) return in.color * gray.sample(s, in.uv).r;   // premultiplied tint
        return color.sample(s, in.uv) * in.color.a;                    // emoji, premultiplied
    }
    """
}
