import Foundation
let envd = ProcessInfo.processInfo.environment
let v = envd["FAAS_FOO"]
print("swift:env:\(v!)")
