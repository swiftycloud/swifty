package main

import (
	"os"
	"fmt"
	"math/rand"
	"time"
	"strconv"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
var symbols = []rune("0123456789_.-=+!@#$%^&*?:;")

func randString(n int) (string, error) {
	idx := make([]byte, n)

	_, err := rand.Read(idx)
	if err != nil {
		return "", err
	}

	res := make([]rune, n)
	for i, j := range idx {
		res[i] = letters[int(j) % len(letters)]
		if i == 0 {
			letters = append(letters, symbols...)
		}
	}

	return string(res), nil
}

func main() {
	if len(os.Args) < 3 {
		fmt.Printf("Usage: %s KEY LEN\n", os.Args[0])
		os.Exit(1)
	}

	ln, err := strconv.Atoi(os.Args[2])
	if err != nil {
		fmt.Printf("Can't get secret length: %s\n", err.Error())
		os.Exit(1)
	}

	str, err := randString(ln)
	if str == "" {
		fmt.Printf("Can't generate string: %s\n", err.Error())
		os.Exit(1)
	}

	fmt.Printf("\"%s\": \"%s\"\n", os.Args[1], str)
}
