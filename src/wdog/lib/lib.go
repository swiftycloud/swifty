/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package swifty

import (
	"os"
	"sync"
	"time"
	"errors"
	"strings"
	"encoding/json"
	"encoding/base64"
	"crypto"
	_ "crypto/sha256"
	"crypto/hmac"
	"gopkg.in/mgo.v2"
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/aws/credentials"
)

func mwareName(mwn string) string {
	return strings.ToUpper(strings.Replace(mwn, ".", "", -1))
}

var _mgoSessions sync.Map

func MongoDatabase(mwn string) (*mgo.Database, error) {
	var ses *mgo.Session

	mwn = mwareName(mwn)
	dbn := os.Getenv("MWARE_MONGO" + mwn + "_DBNAME")
	if dbn == "" {
		return nil, errors.New("Middleware not attached")
	}

	sv, ok := _mgoSessions.Load(mwn)
	if !ok {
		addr := os.Getenv("MWARE_MONGO" + mwn + "_ADDR")
		user := os.Getenv("MWARE_MONGO" + mwn + "_USER")
		pass := os.Getenv("MWARE_MONGO" + mwn + "_PASS")

		info := mgo.DialInfo{
			Addrs:          []string{addr},
			Database:       dbn,
			Username:       user,
			Password:       pass,
		}

		var err error
		ses, err = mgo.DialWithInfo(&info);
		if err != nil {
			return nil, err
		}

		sv, ok = _mgoSessions.LoadOrStore(mwn, ses)
		if ok {
			ses.Close()
		}
	}

	ses = sv.(*mgo.Session)
	return ses.DB(dbn), nil
}

var _mariaDBS sync.Map
var db *sql.DB

func MariaConn(mwn string) (*sql.DB, error) {
	mwn = mwareName(mwn)
	dbv, ok := _mariaDBS.Load(mwn)
	if !ok {
		dbn := os.Getenv("MWARE_MARIA" + mwn + "_DBNAME")
		if dbn == "" {
			return nil, errors.New("Middleware not attached")
		}

		addr := os.Getenv("MWARE_MARIA" + mwn + "_ADDR")
		user := os.Getenv("MWARE_MARIA" + mwn + "_USER")
		pass := os.Getenv("MWARE_MARIA" + mwn + "_PASS")

		db, err := sql.Open("mysql", user + ":" + pass + "@tcp(" + addr + ")/" + dbn)
		if err != nil {
			return nil, err
		}

		err = db.Ping()
		if err != nil {
			return nil, err
		}

		dbv, ok = _mariaDBS.LoadOrStore(mwn, db)
		if ok {
			db.Close()
		}
	}

	return dbv.(*sql.DB), nil
}

type AuthCtx struct {
	UsersCol	*mgo.Collection
	signKey		string
}

var _authCtx *AuthCtx

func AuthContext() (*AuthCtx, error) {
	ctx := _authCtx
	if ctx == nil {
		var err error

		ctx = &AuthCtx{}

		aun := os.Getenv("SWIFTY_AUTH_NAME")
		if aun == "" {
			return nil, errors.New("No authjwt middleware attached")
		}

		db, err := MongoDatabase(aun + "_mgo")
		if err != nil {
			return nil, errors.New("No mongo for authjwn found")
		}

		key := os.Getenv("MWARE_AUTHJWT" + mwareName(aun + "_jwt") + "_SIGNKEY")
		if key == "" {
			return nil, errors.New("No authjwt key found")
		}

		ctx.UsersCol = db.C("Users")
		ctx.signKey = key
		_authCtx = ctx
	}
	return ctx, nil
}

func encodeBytes(b []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(b), "=")
}

func (ctx *AuthCtx)MakeJWT(claims map[string]interface{}) (string, error) {
	header, _ := json.Marshal(map[string]string {
		"typ": "JWT",
		"alg": "HS256",
	})

	claims["iat"] = time.Now().Unix()
	claimsJ, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	unsigned := encodeBytes(header) + "." + encodeBytes(claimsJ)
	hasher := hmac.New(crypto.SHA256.New, []byte(ctx.signKey))
	hasher.Write([]byte(unsigned))

	return unsigned + "." + encodeBytes(hasher.Sum(nil)), nil
}

func S3Bucket(bname string) (*s3.S3, error) {
	return S3BucketProt(bname, "https")
}

func S3BucketProt(bname, prot string) (*s3.S3, error) {
	bname = mwareName(bname)
	addr := os.Getenv("MWARE_S3" + bname + "_ADDR")
	akey := os.Getenv("MWARE_S3" + bname + "_KEY")
	asec := os.Getenv("MWARE_S3" + bname + "_SECRET")

	if addr == "" || akey == "" || asec == "" {
		return nil, errors.New("No bucket attached")
	}

	ses := session.Must(session.NewSessionWithOptions(session.Options{
		Config: aws.Config {
			Region: aws.String("internal"),
			Credentials: credentials.NewStaticCredentials(akey, asec, ""),
			Endpoint: aws.String(prot + "://" + addr),
			S3ForcePathStyle: aws.Bool(true),
			S3UseAccelerate: aws.Bool(false),
		},
	}))

	return s3.New(ses), nil
}
