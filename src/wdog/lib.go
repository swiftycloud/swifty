package swifty

import (
	"os"
	"sync"
	"errors"
	"strings"
	"gopkg.in/mgo.v2"
)

var _mgoSessions sync.Map

func MongoDatabase(mwn string) (*mgo.Database, error) {
	var ses *mgo.Session

	mwn = strings.ToUpper(mwn)
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
