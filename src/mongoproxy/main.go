package main

func main() {
	pipelineAdd(&rqShow{})

	p := makeProxy(":27018", "127.0.0.1:27017")
	if p == nil {
		return
	}

	defer p.Close()

	p.Run()
}
