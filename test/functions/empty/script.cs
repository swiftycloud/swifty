using System;

class Message
{
	public string msg;
}

class Function
{
	static public (Message, Response) Main (Request req)
	{
		var ret = new Message();
		ret.msg = "Hello, world!";
		return (ret, null);
	}
}

