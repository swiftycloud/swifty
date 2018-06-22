#!/usr/local/bin/ruby

require 'json'

begin
require '/function/script.rb'
def CallMain(args)
	res = Main(args)
	return "0:" + JSON.generate(res)
end
rescue ScriptError
def CallMain(args)
	return "2:Error loading script"
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
	args = JSON.parse(str)

	begin
		ret = CallMain(args)
	rescue
		puts "Exception running FN"
		ret = "1:Exception"
	end

	send(queue, ret)
end
