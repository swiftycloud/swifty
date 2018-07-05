package main

func Main(rq *Request) (interface{}, *Responce) {
	return map[string]string{"message": "hw:golang:" + rq.Args["name"]}, nil
}
