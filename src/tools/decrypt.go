/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"swifty/common/crypto"
	"encoding/hex"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Printf("Usage: %s <secret-value> <secret-password>\n", os.Args[0])
		fmt.Printf("       secret-value:    taken from DB\n")
		fmt.Printf("       secret-password: taken from gate secrets file\n")
		return
	}

	pass, err := hex.DecodeString(os.Args[2])
	if err != nil {
		fmt.Printf("Error decoding password: %s\n", err.Error())
		return
	}

	dec, err := xcrypt.DecryptString(pass, os.Args[1])
	if err != nil {
		fmt.Printf("Error decrypting value: %s\n", err.Error())
		return
	}

	fmt.Printf("%s\n", dec)
}
