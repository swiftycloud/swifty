/*
 * Sample user profiles management function that uses swifty JWT auth.
 *
 * How to use:
 *
 * 1. Create authentication-as-a-service
 * 2. Create "profiles" middleware of type mongo
 * 3. Register and configure this function
 *    - add call authentication from step 1
 *    - add "profiles" middleware from step 2
 *    - add "url" event trigger (of any name)
 *
 * Now your users can have their profiles, but prior to this
 * they need to authenticate
 *
 * 1. Signup a user
 *    curl -X POST '$AUTH_FN_URL?action=signup&userid=$NAME&password=$PASS'
 * 2. Sign in and grab the JWT
 *    curl -X POST '$AUTH_FN_URL?action=signin&userid=$NAME&password=$PASS'
 *
 * Now call this FN with obtained JWT
 *
 * -. Create user profile
 *    curl -X POST -H 'Authorization: Bearer $USER_JWT' '$THIS_FN_URL?action=create' -d '$JSON_WITH_PROFILE'
 * -. Check user profile
 *    curl -X POST -H 'Authorization: Bearer $USER_JWT' '$THIS_FN_URL?action=get'
 * -. Update user profile
 *    curl -X POST -H 'Authorization: Bearer $USER_JWT' '$THIS_FN_URL?action=update' -d '$JSON_WITH_UPDATE'
 *    The json string with update IS the list of fields to be pdated, the
 *    whole profile is NOT replaced with the new value.
 *
 * The code below keeps user profiles in DB "profiles" collection "data". You
 * can change any of it. E.g. you can use the step 1 auth's DB as your profiles
 * DB, for this you don't need to create and attach the dedicated middleware
 * and should rename the DBname in the code below into your auth's one.
 */

package main

import (
	"fmt"
	"swifty"
	"encoding/json"
	"gopkg.in/mgo.v2/bson"
)

func pError(err string) map[string]string {
	return map[string]string{"status": "error", "error": err}
}

func Main(args map[string]string) interface{} {
	db, err := swifty.MongoDatabase("profiles")
	if err != nil {
		fmt.Println(err)
		panic("Can't get mgo dbase")
	}

	var claims map[string]interface{}

	err = json.Unmarshal([]byte(args["_SWY_JWT_CLAIMS_"]), &claims)
	if err != nil {
		fmt.Println(err)
		panic("Can't unmarshal claims")
	}

	var profile map[string]interface{}

	if args["action"] == "get" {
		err = db.C("data").Find(bson.M{"cookie": claims["cookie"]}).One(&profile)
		if err != nil {
			return pError(err.Error())
		}

		return profile
	}

	if args["action"] == "delete" {
		err = db.C("data").Remove(bson.M{"cookie": claims["cookie"]})
		if err != nil {
			return pError(err.Error())
		}

		return "OK"
	}

	err = json.Unmarshal([]byte(args["_SWY_BODY_"]), &profile)
	if err != nil {
		return pError(err.Error())
	}

	if args["action"] == "create" {
		/*
		 * The defaul auth function generates "cookie" field in the
		 * claims that contain unique user ID. This ID is now the key
		 * in profiles collection.
		 */
		profile["cookie"] = claims["cookie"]
		err = db.C("data").Insert(profile)
		if err != nil {
			return pError(err.Error())
		}

		return "OK"
	}

	if args["action"] == "update" {
		profile["cookie"] = claims["cookie"]
		err = db.C("data").Update(bson.M{"cookie": claims["cookie"]},
				bson.M{"$set": profile})
		if err != nil {
			return pError(err.Error())
		}

		return "OK"
	}

	return pError("Bad action")
}
