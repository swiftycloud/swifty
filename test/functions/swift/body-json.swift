import Foundation

func Main(args: [String:String]) -> Encodable {
	let body = args["SWY_BODY"]!
	let dat = Data(bytes: Array(body.utf8))
	let obj = try! JSONDecoder().decode([String:String].self, from: dat)
	return ["status": obj["status"]!]
}
