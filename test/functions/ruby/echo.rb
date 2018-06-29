def Main(req)
	puts req
	STDOUT.flush
	return { "message" => "hw:ruby:" + req.args["name"] }
end
