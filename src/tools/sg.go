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
	"encoding/base64"
	"flag"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
var symbols = []rune("0123456789_.-=+!@#$%^&*?:;")

func code(data []byte, b64 bool) string {
	if b64 {
		return base64.StdEncoding.EncodeToString(data)
	} else {
		return hex.EncodeToString(data)
	}
}

func randBytes(n int, b64 bool) (string, error) {
	idx := make([]byte, n)
	_, err := rand.Read(idx)
	if err != nil {
		return "", err
	}

	return code(idx, b64), nil
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
	var bytes, b64 bool
	var length int
	var name, str, rec string
	var err error

	flag.BoolVar(&bytes, "b", false, "gen bytes secret")
	flag.BoolVar(&b64, "B", false, "use base64 encoding")
	flag.IntVar(&length, "l", 16, "len of the key")
	flag.StringVar(&name, "n", "SECRET", "name of the key")
	flag.StringVar(&rec, "r", "EXISTING", "existing secret (to recode)")
	flag.Parse()

	if rec != "" {
		var d []byte
		d, err = hex.DecodeString(rec)
		str = code(d, b64)
	} else if bytes {
		str, err = randBytes(length, b64)
	} else {
		str, err = randString(length)
	}

	if str == "" {
		fmt.Printf("Can't generate string: %s\n", err.Error())
		os.Exit(1)
	}

	fmt.Printf("\"%s\": \"%s\"\n", name, str)
}
