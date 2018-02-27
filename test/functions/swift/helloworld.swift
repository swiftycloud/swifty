func Main(args: [String:String]) -> Encodable {
	return ["message": "hw:swift:" + args["name"]!]
}
