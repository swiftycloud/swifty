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
 *    curl -X PUT -H 'Authorization: Bearer $USER_JWT' '$THIS_FN_URL' -H 'Content-type: application/json' -d '$JSON_WITH_PROFILE'
 * -. Check user profile
 *    curl -X GET -H 'Authorization: Bearer $USER_JWT' '$THIS_FN_URL'
 * -. Update user profile
 *    curl -X POST -H 'Authorization: Bearer $USER_JWT' '$THIS_FN_URL' -H 'Content-type: application/json' -d '$JSON_WITH_UPDATE'
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

func Main(rq *Request) (interface{}, *Responce) {
	db, err := swifty.MongoDatabase("profiles")
	if err != nil {
		fmt.Println(err)
		panic("Can't get mgo dbase")
	}

	var profile map[string]interface{}

	if rq.Method == "GET" {
		err = db.C("data").Find(bson.M{"cookie": rq.Claims["cookie"]}).One(&profile)
		if err != nil {
			return pError(err.Error()), nil
		}

		return profile, nil
	}

	if rq.Method == "DELETE" {
		err = db.C("data").Remove(bson.M{"cookie": rq.Claims["cookie"]})
		if err != nil {
			return pError(err.Error()), nil
		}

		return "OK", nil
	}

	err = json.Unmarshal([]byte(rq.Body), &profile)
	if err != nil {
		return pError(err.Error()), nil
	}

	if rq.Method == "POST" {
		/*
		 * The defaul auth function generates "cookie" field in the
		 * claims that contain unique user ID. This ID is now the key
		 * in profiles collection.
		 */
		profile["cookie"] = rq.Claims["cookie"]
		err = db.C("data").Insert(profile)
		if err != nil {
			return pError(err.Error()), nil
		}

		return "OK", nil
	}

	if rq.Method == "PUT" {
		profile["cookie"] = rq.Claims["cookie"]
		err = db.C("data").Update(bson.M{"cookie": rq.Claims["cookie"]},
				bson.M{"$set": profile})
		if err != nil {
			return pError(err.Error()), nil
		}

		return "OK", nil
	}

	return pError("Bad action"), nil
}
