/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"fmt"
	"time"
	"errors"
	"strings"
	"encoding/json"
	"encoding/base64"
	"swifty/common"
	"swifty/apis"
	"net/http"
	"crypto"
	_ "crypto/sha256"
	"crypto/hmac"
	"context"
)

const (
	authLabel = "auth"
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

func authCtxGet(ctx context.Context, id SwoId, ac string) (*AuthCtx, error) {
	var item MwareDesc

	id.Name = ac
	err := dbFind(ctx, id.dbReq(), &item)
	if err != nil {
		return nil, err
	}
	if item.State != DBMwareStateRdy {
		return nil, errors.New("Mware not ready")
	}

	if item.MwareType == "authjwt" {
		key, err := xh.DecryptString(gateSecPas, item.Secret)
		if err != nil {
			return nil, err
		}

		return &AuthCtx{signKey: key}, nil
	}

	return nil, fmt.Errorf("BUG: Not an auth mware %s", item.MwareType)
}

func (ac *AuthCtx)Verify(r *http.Request) (map[string]interface{}, error) {
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

	var claims map[string]interface{}
	err = json.Unmarshal(cb, &claims)
	if err != nil {
		return nil, errors.New("Bad JWT claims: " + err.Error())
	}

	expf, ok := claims["exp"]
	if ok {
		now := time.Now().Unix()
		var exp int64

		switch expt := expf.(type) {
		case float64:
			exp = int64(expt)
		case json.Number:
			exp, _ = expt.Int64()
		default:
			now = 0
			exp = 1 /* XXX valid? why not? */
		}

		if exp <= now {
			return nil, errors.New("Token expired")
		}
	}

	return claims, nil
}

func InitAuthJWT(ctx context.Context, mwd *MwareDesc) (error) {
	var err error

	mwd.Secret, err = xh.GenRandId(32)
	if err != nil {
		return err
	}

	return nil
}

func FiniAuthJWT(ctx context.Context, mwd *MwareDesc) error {
	return nil
}

func GetEnvAuthJWT(ctx context.Context, mwd *MwareDesc) map[string][]byte {
	return map[string][]byte{mwd.envName("SIGNKEY"): []byte(mwd.Secret)}
}

func TInfoAuthJWT(ctx context.Context) *swyapi.MwareTypeInfo {
	return &swyapi.MwareTypeInfo {
		Envs: []string {
			mkEnvName("authjwt", "%name%", "SIGNKEY"),
		},
	}
}

var MwareAuthJWT = MwareOps {
	Init:	InitAuthJWT,
	Fini:	FiniAuthJWT,
	GetEnv:	GetEnvAuthJWT,
	TInfo:	TInfoAuthJWT,
	LiteOK:	true,
}
