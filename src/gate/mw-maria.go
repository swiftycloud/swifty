package main

import (
	"encoding/json"
	"fmt"
	"database/sql"
	_ "github.com/go-sql-driver/mysql"

	"../apis/apps"
)

type DBSettings struct {
	DBName		string			`json:"dbname"`
}

func mariaConn(conf *YAMLConfMw) (*sql.DB, error) {
	return sql.Open("mysql",
			fmt.Sprintf("%s:%s@tcp(%s)/?charset=utf8",
				conf.Maria.Admin,
				conf.Maria.Pass,
				conf.Maria.Addr))
}

func mariaReq(db *sql.DB, req string) error {
	_, err := db.Exec(req)
	if err != nil {
		return fmt.Errorf("DB: cannot execure %s req: %s", req, err.Error())
	}

	return nil
}

// SELECT User FROM mysql.user;
// SHOW DATABASES;
// DROP USER IF EXISTS '8257fbff9618952fbd2b83b4794eb694'@'%';
// DROP DATABASE IF EXISTS 8257fbff9618952fbd2b83b4794eb694;

func InitMariaDB(conf *YAMLConfMw, mwd *MwareDesc, mware *swyapi.MwareItem) (error) {
	dbs := DBSettings{ }

	err := mwareGenerateClient(mwd)
	if err != nil {
		return err
	}

	dbs.DBName = mwd.Client

	db, err := mariaConn(conf)
	if err != nil {
		return err
	}
	defer db.Close()

	err = mariaReq(db, "CREATE USER '" + mwd.Client + "'@'%' IDENTIFIED BY '" + mwd.Pass + "';")
	if err != nil {
		return err
	}

	err = mariaReq(db, "CREATE DATABASE " + dbs.DBName + " CHARACTER SET utf8 COLLATE utf8_general_ci;")
	if err != nil {
		return err
	}

	err = mariaReq(db, "GRANT ALL PRIVILEGES ON " + dbs.DBName + ".* TO '" + mwd.Client + "'@'%' IDENTIFIED BY '" + mwd.Pass + "';")
	if err != nil {
		return err
	}

	js, err := json.Marshal(&dbs)
	if err != nil {
		return err
	}

	mwd.JSettings = string(js)

	return nil
}

func FiniMariaDB(conf *YAMLConfMw, mwd *MwareDesc) error {
	var dbs DBSettings

	err := json.Unmarshal([]byte(mwd.JSettings), &dbs)
	if err != nil {
		return fmt.Errorf("MariaDBSettings.Fini: Can't unmarshal data %s: %s",
					mwd.JSettings, err.Error())
	}

	db, err := mariaConn(conf)
	if err != nil {
		return err
	}
	defer db.Close()

	err = mariaReq(db, "DROP USER IF EXISTS '" + mwd.Client + "'@'%';")
	if err != nil {
		log.Errorf("maria: can't drop user %s: %s", mwd.Client, err.Error())
	}

	err = mariaReq(db, "DROP DATABASE IF EXISTS " + dbs.DBName + ";")
	if err != nil {
		log.Errorf("maria: can't drop database %s: %s", dbs.DBName, err.Error())
	}

	return nil
}

func GetEnvMariaDB(conf *YAMLConfMw, mwd *MwareDesc) ([]string) {
	var dbs DBSettings
	var envs []string
	var err error

	err = json.Unmarshal([]byte(mwd.JSettings), &dbs)
	if err == nil {
		envs = append(mwGenEnvs(mwd, conf.Maria.Addr), mkEnv(mwd, "DBNAME", dbs.DBName))
	} else {
		log.Fatal("rabbit: Can't unmarshal DB entry %s", mwd.JSettings)
	}

	return envs
}

var MwareMariaDB = MwareOps {
	Init:	InitMariaDB,
	Fini:	FiniMariaDB,
	GetEnv:	GetEnvMariaDB,
	Devel:	true,
}

