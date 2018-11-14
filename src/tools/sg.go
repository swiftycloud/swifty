/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"os"
	"fmt"
	"math/rand"
	"time"
	"encoding/hex"
	"flag"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
var symbols = []rune("0123456789_.-=+!@#$%^&*?:;")

func randBytes(n int) (string, error) {
	idx := make([]byte, n)
	_, err := rand.Read(idx)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(idx), nil
}

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
	var bytes bool
	var length int
	var name, str string
	var err error

	flag.BoolVar(&bytes, "b", false, "gen bytes secret")
	flag.IntVar(&length, "l", 16, "len of the key")
	flag.StringVar(&name, "n", "SECRET", "name of the key")
	flag.Parse()

	if bytes {
		str, err = randBytes(length)
	} else {
		str, err = randString(length)
	}

	if str == "" {
		fmt.Printf("Can't generate string: %s\n", err.Error())
		os.Exit(1)
	}

	fmt.Printf("\"%s\": \"%s\"\n", name, str)
}
