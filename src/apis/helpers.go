package swyapi

func (cln *Client)Add(url string, succ int, in interface{}, out interface{}) {
	cln.Req1("POST", url, succ, in, out)
}

func (cln *Client)List(url string, succ int, out interface{}) {
	cln.Req1("GET", url, succ, nil, out)
}

func (cln *Client)Get(url string, succ int, out interface{}) {
	cln.Req1("GET", url, succ, nil, out)
}

func (cln *Client)Mod(url string, succ int, in interface{}) {
	cln.Req1("PUT", url, succ, in, nil)
}

func (cln *Client)Del(url string, succ int) {
	cln.Req1("DELETE", url, succ, nil, nil)
}

