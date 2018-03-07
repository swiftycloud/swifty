package main

func Main(args map[string]string) interface{} {
	if args["act"] == "panic" {
		panic(args["message"])
	} else {
		print(args["act"])
	}

	return "done"
}
