package swycrypt

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

func aesPad(buf []byte) []byte {
	padsz := aes.BlockSize - len(buf)%aes.BlockSize
	return append(buf, bytes.Repeat([]byte{byte(padsz)}, padsz)...)
}

func aesUnpad(buf []byte) []byte {
	l := len(buf)
	padsz := int(buf[l - 1])
	if padsz > l {
		return nil
	}
	return buf[:(l - padsz)]
}

func EncryptString(key []byte, text string) (string, error) {
	msg := aesPad([]byte(text))

	if len(key) > 16 {
		key = key[:16]
	}

	ciphermsg := make([]byte, aes.BlockSize + len(msg))
	nonce := ciphermsg[:aes.BlockSize]
	_, err := io.ReadFull(rand.Reader, nonce)
	if err != nil {
		return "", fmt.Errorf("Can't get nonce: %s", err.Error())
	}

	aesc, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("Can't setup aes: %s", err.Error())
	}

	encr := cipher.NewCFBEncrypter(aesc, nonce)
	encr.XORKeyStream(ciphermsg[aes.BlockSize:], []byte(msg))

	return hex.EncodeToString(ciphermsg), nil
}

func DecryptString(key []byte, ciphertext string) (string, error) {
	if len(key) > 16 {
		key = key[:16]
	}

	ciphermsg, err := hex.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("Can't decode cipher text: %s", err.Error())
	}

	if (len(ciphermsg) % aes.BlockSize) != 0 {
		return "", errors.New("Bad cipher text size")
	}

	nonce := ciphermsg[:aes.BlockSize]
	msg := ciphermsg[aes.BlockSize:]

	aesc, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("Can't setup aes: %s", err.Error())
	}

	decr := cipher.NewCFBDecrypter(aesc, nonce)
	decr.XORKeyStream(msg, msg)

	umsg := aesUnpad(msg)
	if umsg == nil {
		return "", errors.New("Decoded message unpad error")
	}

	return string(umsg), nil
}
