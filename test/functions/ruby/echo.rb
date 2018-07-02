def Main(req)
	puts req
	puts req.args
	puts req.claims
	STDOUT.flush
	return { "message" => "hw:ruby:" + req.args["name"] }
end
