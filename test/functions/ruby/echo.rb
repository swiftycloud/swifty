def Main(req)
	puts req
	puts req.args
	puts req.claims
	STDOUT.flush
	return { "myname" => "hw:ruby:" + req.args["name"] }, { "status"=>201 }
end
