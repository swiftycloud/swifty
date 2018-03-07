package main

func Main(args map[string]string) interface{} {
	return map[string]string{"message": "hw:golang:" + args["name"]}
}
