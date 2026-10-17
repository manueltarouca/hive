import CoreGraphics
import Foundation
// Prints the window ID of a process's largest on-screen window, or with
// "small", its smallest (a child panel such as the command palette). The
// process is a PID, so other running copies of the app are never picked.
let pid = Int(CommandLine.arguments[1]) ?? -1
let small = CommandLine.arguments.count > 2 && CommandLine.arguments[2] == "small"
let list = CGWindowListCopyWindowInfo([.optionOnScreenOnly], kCGNullWindowID) as? [[String: Any]] ?? []
let mine = list.compactMap { w -> (Int, CGFloat)? in
    guard (w[kCGWindowOwnerPID as String] as? Int) == pid, let d = w[kCGWindowBounds as String] as? NSDictionary,
          let r = CGRect(dictionaryRepresentation: d), r.width > 50 else { return nil }
    return (w[kCGWindowNumber as String] as! Int, r.width * r.height)
}.sorted { $0.1 < $1.1 }
guard let pick = small ? mine.first : mine.last else { exit(1) }
print(pick.0)
