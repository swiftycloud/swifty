package main

import (
	"gopkg.in/yaml.v2"
)

func Main(rq *Request) (interface{}, *Response) {
	x, _ := yaml.Marshal([]string{"foo", "bar"})
	return string(x), nil
}
