/*
 * Simple user management for swifty Auth-as-a-Service
 *
 * This FN is typically activated as a part of Auth deployment created
 * by the PUT /auths API call.
 *
 * API in this FN is
 *
 * curl 'swifty.cloud:8686/call/{fnid}/signup?userid={userid}&password={password}'
 * curl 'swifty.cloud:8686/call/{fnid}/signin?userid={userid}&password={password}'
 * curl 'swifty.cloud:8686/call/{fnid}/leave?userid={userid}&password={password}'
 *
 * The exact http method isn't checked and doesn't matter.
 */

package main

import (
	"fmt"
	"swifty"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type authResp struct {
	Error	string	`json:"error,omitempty"`
	Token	string	`json:"token,omitempty"`
}

func doSignup(auth *swifty.AuthCtx, args map[string]string) interface{} {
	pwdHash, err := bcrypt.GenerateFromPassword([]byte(args["password"]), 0)
	if err != nil {
		fmt.Printf("Error hashing pass: %s", err.Error())
		return &authResp{Error: "Error registering"}
	}
	err = auth.UsersCol.Insert(bson.M{ "userid": args["userid"], "password": pwdHash })
	if err != nil {
		fmt.Printf("Error inserting: %s", err.Error())
		return &authResp{Error: "Error registering"}
	}

	return &authResp{}
}

func doSignin(auth *swifty.AuthCtx, args map[string]string) interface{} {
	var urec map[string]interface{}

	err := auth.UsersCol.Find(bson.M{"userid": args["userid"]}).One(&urec)
	if err != nil {
		if err == mgo.ErrNotFound {
			return &authResp{Error: "Invalid credentials"}
		}

		fmt.Printf("Error signing up: %s", err.Error())
		return &authResp{Error: "Error signing in"}
	}

	err = bcrypt.CompareHashAndPassword(urec["password"].([]byte), []byte(args["password"]))
	if err != nil {
		return &authResp{Error: "Invalid credentials"}
	}

	jwt, err := auth.MakeJWT(map[string]interface{}{ "userid": args["userid"], "cookie": urec["_id"] })
	if err != nil {
		return &authResp{Error: "Error signing in"}
	}

	return &authResp{Token: jwt}
}

func doLeave(auth *swifty.AuthCtx, args map[string]string) interface{} {
	var urec map[string]interface{}

	err := auth.UsersCol.Find(bson.M{"userid": args["userid"]}).One(&urec)
	if err != nil {
		if err == mgo.ErrNotFound {
			return &authResp{Error: "Invalid credentials"}
		}

		fmt.Printf("Error signing up: %s", err.Error())
		return &authResp{Error: "Error leaving"}
	}

	err = bcrypt.CompareHashAndPassword(urec["password"].([]byte), []byte(args["password"]))
	if err != nil {
		return &authResp{Error: "Invalid credentials"}
	}

	err = auth.UsersCol.Remove(bson.M{"userid": args["userid"], "password": urec["password"]})
	if err != nil {
		return &authResp{Error: "Error leaving"}
	}

	return &authResp{}
}

func Main(req *Request) (interface{}, *Responce) {
	auth, err := swifty.AuthContext()

	if err != nil {
		fmt.Println(err)
		panic("Can't get auth context")
	}

	switch req.Path {
	case "signup":
		return doSignup(auth, req.Args), nil
	case "signin":
		return doSignin(auth, req.Args), nil
	case "leave":
		return doLeave(auth, req.Args), nil
	}

	return "Invalid action", nil
}
