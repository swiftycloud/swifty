package main

func Main(rq *Request) (interface{}, *Response) {
	return map[string]string{"message": "ok"}, nil
}
