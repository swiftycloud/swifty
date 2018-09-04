#!/usr/local/bin/ruby

require 'json'
require 'ostruct'

begin
require '/function/' + ARGV[0] + '.rb'
def CallMain(req)
	res, resp = Main(req)
	ret = {:res => 0, :ret => JSON.generate(res)}
	if resp != nil
		ret["status"] = resp["status"].to_i
	end
	return JSON.generate(ret)
end
rescue ScriptError
def CallMain(req)
	return JSON.generate({:res => 2, :ret => "Error loading script"})
end
end

queue = IO.for_fd 3

def recv(q)
	ret = ""
	loop do
		v = q.readpartial(1024).to_s
		ret += v
		if v.length() < 1024
			return ret
		end
	end
end

def send(q, str)
	loop do
		sub = str[0, 1024]
		q.write(sub)

		str = str[1024..-1]
		if str.nil?
			break
		end
	end
end

loop do
	str = recv(queue)
	req = JSON.parse(str, object_class: OpenStruct)

	begin
		ret = CallMain(req)
	rescue
		puts "Exception running FN"
		ret = "1:Exception"
	end

	send(queue, ret)
end
