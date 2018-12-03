struct Resp: Encodable {
	var msg: String
}

func Main(rq: Request) -> (Encodable, Response?) {
	let result = Resp(msg: "Hello, world")
	return ( result, nil )
}
