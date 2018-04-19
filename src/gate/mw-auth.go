package main

import (
	"fmt"
	"errors"
	"strings"
	"encoding/json"
	"encoding/base64"
	"../common"
	"net/http"
	"../common/crypto"
	"crypto"
	_ "crypto/sha256"
	"crypto/hmac"
	"context"
)

func decodeString(s string) ([]byte, error) {
	if l := len(s) % 4; l > 0 {
		s += strings.Repeat("=", 4-l)
	}

	return base64.URLEncoding.DecodeString(s)
}

type AuthCtx struct {
	signKey		string
}

func authCtxGet(fn *FunctionDesc) (*AuthCtx, error) {
	var id SwoId

	id = fn.SwoId
	id.Name = fn.AuthCtx

	item, err := dbMwareGetReady(&id)
	if err != nil {
		return nil, err
	}

	if item.MwareType == "authjwt" {
		key, err := swycrypt.DecryptString(gateSecPas, item.Secret)
		if err != nil {
			return nil, err
		}

		return &AuthCtx{signKey: key}, nil
	}

	return nil, fmt.Errorf("BUG: Not an auth mware %s", item.MwareType)
}

func (ac *AuthCtx)Verify(r *http.Request) (map[string]string, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return nil, errors.New("Authorization header required")
	}

	ah := strings.SplitN(auth, " ", 2)
	if len(ah) != 2 || ah[0] != "Bearer" {
		return nil, errors.New("Authorization Bearer scheme required")
	}

	parts := strings.Split(ah[1], ".")
	if len(parts) != 3 {
		return nil, errors.New("Bad JWT token")
	}

	hb, err := decodeString(parts[0])
	if err != nil {
		return nil, errors.New("Bad JWT header")
	}

	var h map[string]string
	err = json.Unmarshal(hb, &h)
	if err != nil {
		return nil, errors.New("Bad JWT header")
	}

	/* Should match the wdog/lib.go */
	if h["typ"] != "JWT" || h["alg"] != "HS256" {
		return nil, errors.New("Bad JWT header")
	}

	sig, err := decodeString(parts[2])
	if err != nil {
		return nil, errors.New("Bad JWT signature")
	}

	hasher := hmac.New(crypto.SHA256.New, []byte(ac.signKey))
	hasher.Write([]byte(parts[0] + "." + parts[1]))

	if !hmac.Equal(sig, hasher.Sum(nil)) {
		return nil, errors.New("Wrong JWT signature")
	}

	cb, err := decodeString(parts[1])
	if err != nil {
		return nil, errors.New("Bad JWT claims")
	}

	return map[string]string{SwyJWTClaimsArg: string(cb)}, nil
}

func InitAuthJWT(ctx context.Context, conf *YAMLConfMw, mwd *MwareDesc) (error) {
	var err error

	mwd.Secret, err = swy.GenRandId(32)
	if err != nil {
		return err
	}

	return nil
}

func FiniAuthJWT(ctx context.Context, conf *YAMLConfMw, mwd *MwareDesc) error {
	return nil
}

func GetEnvAuthJWT(conf *YAMLConfMw, mwd *MwareDesc) ([][2]string) {
	return [][2]string{ mkEnv(mwd, "SIGNKEY", mwd.Secret) }
}

var MwareAuthJWT = MwareOps {
	Init:	InitAuthJWT,
	Fini:	FiniAuthJWT,
	GetEnv:	GetEnvAuthJWT,
}