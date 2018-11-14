//
// © 2018 SwiftyCloud OÜ. All rights reserved.
// Info: info@swifty.cloud
//

'use strict';

require('libsys/shim')
require('libjs/shim')
var script = require('/function/' + process.argv[2] + '.js')
var qfd = 3
var buf = Buffer.alloc(1024)

function recv() {
	var ret = ""

	for (;;) {
		var l = libjs.recv(qfd, buf)

		ret += buf.toString('utf8', 0, l)
		if (l < 1024)
			return ret
	}
}

function send(res) {
	if (res.length % 1024 == 0)
		res += "\0"

	while (res.length > 0) {
		var s = res.substring(0, 1024)
		libjs.send(qfd, s)
		res = res.substring(1024)
	}
}

for (;;) {
	var str = recv()
	var req = JSON.parse(str)
	var res
	try {
		var [ ret, resp ] = script.Main(req)
		res = { res: 0, ret: JSON.stringify(ret) }
		if (resp != null) {
			res.status = resp.status
		}
	} catch (err) {
		res = { res: 0, ret: "Exception" }
	}

	send(JSON.stringify(res))
}

process.exit(1)
