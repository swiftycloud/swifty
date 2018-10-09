package main

import (
	"fmt"
	"errors"
	"context"
	"swifty/apis"
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
)

const (
	mariaDefSize = "10240"
	mariaDefRows = "1024"
)

func mariaConn() (*sql.DB, error) {
	return sql.Open("mysql",
			fmt.Sprintf("%s:%s@tcp(%s)/?charset=utf8",
				conf.Mware.Maria.c.User,
				gateSecrets[conf.Mware.Maria.c.Pass],
				conf.Mware.Maria.c.Addr()))
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

func InitMariaDB(ctx context.Context, mwd *MwareDesc) (error) {
	err := mwareGenerateUserPassClient(ctx, mwd)
	if err != nil {
		return err
	}

	mwd.Namespace = mwd.Client

	db, err := mariaConn()
	if err != nil {
		goto out;
	}
	defer db.Close()

	err = mariaReq(db, "CREATE USER '" + mwd.Client + "'@'%' IDENTIFIED BY '" + mwd.Secret + "';")
	if err != nil {
		goto out
	}

	err = mariaReq(db, "CREATE DATABASE " + mwd.Namespace + " CHARACTER SET utf8 COLLATE utf8_general_ci;")
	if err != nil {
		goto outu
	}

	err = mariaReq(db, "GRANT ALL PRIVILEGES ON " + mwd.Namespace + ".* TO '" + mwd.Client + "'@'%' IDENTIFIED BY '" + mwd.Secret + "';")
	if err != nil {
		goto outd
	}

	/* FIXME -- these are random numbers until we decide on quoting policy */
	err = mariaReq(db, "INSERT INTO " + conf.Mware.Maria.QDB + " VALUES ('" + mwd.Namespace + "', " + mariaDefSize + ", " + mariaDefRows + ", false)")
	if err != nil {
		goto outd
	}

	return nil

outd:
	mariaDropDb(ctx, db, mwd)
outu:
	mariaDropUser(ctx, db, mwd)
out:
	return err
}

func mariaDropUser(ctx context.Context, db *sql.DB, mwd *MwareDesc) {
	err := mariaReq(db, "DROP USER IF EXISTS '" + mwd.Client + "'@'%';")
	if err != nil {
		ctxlog(ctx).Errorf("maria: can't drop user %s: %s", mwd.Client, err.Error())
	}
}

func mariaDropDb(ctx context.Context, db *sql.DB, mwd *MwareDesc) {
	err := mariaReq(db, "DROP DATABASE IF EXISTS " + mwd.Namespace + ";")
	if err != nil {
		ctxlog(ctx).Errorf("maria: can't drop database %s: %s", mwd.Namespace, err.Error())
	}
}

func mariaDropQuota(ctx context.Context, conf *YAMLConfMaria, db *sql.DB, mwd *MwareDesc) {
	err := mariaReq(db, "DELETE FROM " + conf.QDB + " WHERE id='" + mwd.Namespace + "';")
	if err != nil {
		ctxlog(ctx).Errorf("maria: can't dereg quota for %s: %s", mwd.Namespace, err.Error())
	}
}

func FiniMariaDB(ctx context.Context, mwd *MwareDesc) error {
	db, err := mariaConn()
	if err != nil {
		return err
	}
	defer db.Close()

	mariaDropQuota(ctx, &conf.Mware.Maria, db, mwd)
	mariaDropUser(ctx, db, mwd)
	mariaDropDb(ctx, db, mwd)

	return nil
}

func GetEnvMariaDB(ctx context.Context, mwd *MwareDesc) map[string][]byte {
	e := mwd.stdEnvs(conf.Mware.Maria.c.Addr())
	e[mwd.envName("DBNAME")] = []byte(mwd.Namespace)
	return e
}

func InfoMariaDB(ctx context.Context, mwd *MwareDesc, ifo *swyapi.MwareInfo) error {
	db, err := mariaConn()
	if err != nil {
		return err
	}
	defer db.Close()

	rows, err := db.Query("SELECT " +
				"SUM(information_schema.TABLES.DATA_LENGTH + information_schema.TABLES.INDEX_LENGTH) " +
				"FROM information_schema.TABLES " +
				"WHERE information_schema.TABLES.TABLE_SCHEMA=? " +
				"GROUP BY information_schema.TABLES.TABLE_SCHEMA",
				mwd.Namespace)
	if err != nil {
		ctxlog(ctx).Errorf("Error getting maria DB size: %s", err.Error())
		return errors.New("Error getting DB size")
	}
	defer rows.Close()

	var size uint64
	for rows.Next() {

		if err := rows.Scan(&size); err != nil {
			ctxlog(ctx).Errorf("Can't get result: %s", err.Error())
			return errors.New("Error getting DB size")
		}

		break /* There should be only one */
	}

	ifo.SetDU(size)
	return nil
}

var MwareMariaDB = MwareOps {
	Init:	InitMariaDB,
	Fini:	FiniMariaDB,
	GetEnv:	GetEnvMariaDB,
	Info:	InfoMariaDB,
}

