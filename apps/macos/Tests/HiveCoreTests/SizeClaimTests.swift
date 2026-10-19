import Testing
@testable import HiveCore

/// Who sizes a pane: the device you're using. A reload never takes a pane
/// another device holds; your own input, focus or window does.
@Suite struct SizeClaimTests {
    @Test func nobodyHoldsIt() {
        #expect(SizeClaim.should(owner: nil, me: "a", interactive: false, sameSize: false))
        #expect(SizeClaim.should(owner: "", me: "a", interactive: false, sameSize: false))
        #expect(SizeClaim.should(owner: "cli", me: "a", interactive: false, sameSize: false))
    }

    @Test func anotherDeviceHoldsIt() {
        // The phone fitted it: the Mac's reloads leave it be.
        #expect(!SizeClaim.should(owner: "phone", me: "mac", interactive: false, sameSize: false))
        // Typing, clicking or focusing the window here takes it back.
        #expect(SizeClaim.should(owner: "phone", me: "mac", interactive: true, sameSize: false))
    }

    @Test func alreadyMine() {
        #expect(!SizeClaim.should(owner: "mac", me: "mac", interactive: true, sameSize: true))
        #expect(SizeClaim.should(owner: "mac", me: "mac", interactive: false, sameSize: false))
    }
}
