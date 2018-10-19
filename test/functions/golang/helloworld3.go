package main

type Body struct {
	Name	string
}

func Main(rq *Request) (interface{}, *Responce) {
	return map[string]string{"message": "hw:golang:" + rq.B.Name}, nil
}
