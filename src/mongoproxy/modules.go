package main

type module interface {
	request(string, *mongo_req) error
	config(map[string]interface{}, *Config) error
}

var modules map[string]module = map[string]module {
	"show":	&rqShow{},
	"quota": &quota{},
}
