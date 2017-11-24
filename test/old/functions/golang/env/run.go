package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("golang:env:" + os.Getenv("FAAS_FOO"))
}
