func Main(rq: Request) -> Encodable {
	return ["message": "hw:swift:" + rq.args!["name"]!]
}
