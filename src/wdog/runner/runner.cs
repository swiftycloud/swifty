//
// © 2018 SwiftyCloud OÜ. All rights reserved.
// Info: info@swifty.cloud
//

using System;
using System.Runtime.InteropServices;
using System.Collections.Generic;
using System.Web.Script.Serialization;
using XStream;

public class Request {
	public Dictionary<string, string> Args;
}

public class Response {
	public int status;
	// The "then" thing is here
}

class RunnerRes {
	public int res;
	public string ret;
	public int status;
}

class FR
{
	static private byte[] ToBytes(string str)
	{
		return System.Text.Encoding.ASCII.GetBytes(str);
	}

	static public void Main (string[] args)
	{
		var serializer = new JavaScriptSerializer();
		var q = new XStreamFD(3, false);
		var buf = new byte[1024];

		while (true) {
			var len = q.Read(buf, 0, 1024);
			var str = System.Text.Encoding.UTF8.GetString(buf, 0, len);
			var req = serializer.Deserialize<Request>(str);
			var res = new RunnerRes();

			try {
				var result = Function.Main(req);
				res.res = 0;
				res.ret = serializer.Serialize(result.Item1);

				if (result.Item2 != null)
					res.status = result.Item2.status;
			} catch {
				res.res = 1;
				res.ret = "Exception";
			}

			var res_j = serializer.Serialize(res);
			var res_b = ToBytes(res_j);
			q.Write(res_b, 0, res_b.Length);
		}
	}
}
