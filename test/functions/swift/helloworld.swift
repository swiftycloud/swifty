func Main(rq: Request) -> (Encodable, Response?) {
	return ( ["message": "hw:swift:" + rq.args!["name"]!], nil )
}
