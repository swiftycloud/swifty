package main

import (
	"fmt"
	"swifty"
)

func Main(args map[string]string) interface{} {
	db, err := swifty.MariaConn(args["dbname"])
	if err != nil {
		fmt.Println(err)
		panic("Can't get maria conn")
	}

	res := "invalid"
	if args["action"] == "create" {
		_, err = db.Exec("CREATE TABLE `data` (`key` varchar(255), `val` varchar(255))")
		if err != nil {
			fmt.Println(err)
			panic("Can't create")
		}

		res = "done"
	} else if args["action"] == "insert" {
		stmt, err := db.Prepare("INSERT INTO `data`(`key`, `val`) VALUES(?,?)")
		if err != nil {
			fmt.Println(err)
			panic("Can't prepare insert")
		}

		_, err = stmt.Exec(args["key"], args["val"])
		if err != nil {
			fmt.Println(err)
			panic("Can't insert")
		}

		res = "done"
	} else if args["action"] == "select" {
		stmt, err := db.Prepare("SELECT `val` FROM `data` WHERE `key` = ?")
		if err != nil {
			fmt.Println(err)
			panic("Can't prepare select")
		}

		rows, err := stmt.Query(args["key"])
		if err != nil {
			fmt.Println(err)
			panic("Can't query foo")
		}
		defer rows.Close()

		for rows.Next() {
			var value string

			err = rows.Scan(&value)
			if err != nil {
				panic("No rows")
			}

			res = value
			break
		}
	}

	return map[string]string{"res": res}
}
