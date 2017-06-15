package main

import (
	"strings"
	"fmt"
	"os"
)

func main() {
	fmt.Println("golang:arg:" + strings.Join(os.Args[1:], "."))
}
