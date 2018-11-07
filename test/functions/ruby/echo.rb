def Main(req)
	puts "req:"
	puts req
	puts "args:"
	puts req.args
	puts "content:"
	puts req.content
	puts "body:"
	puts req.body
	puts "b:"
	puts req.b
	if req.b != nil
		puts "b.name:"
		puts req.b.name
	end
	STDOUT.flush
	return { "myname" => "hw:ruby:" + req.args["name"] }, { "status"=>201 }
end
