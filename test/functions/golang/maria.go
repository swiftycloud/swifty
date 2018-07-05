package main

import (
	"fmt"
	"swifty"
)

func Main(rq *Request) (interface{}, *Responce) {
	db, err := swifty.MariaConn(rq.Args["dbname"])
	if err != nil {
		fmt.Println(err)
		panic("Can't get maria conn")
	}

	res := "invalid"
	if rq.Args["action"] == "create" {
		_, err = db.Exec("CREATE TABLE `data` (`key` varchar(255), `val` varchar(255))")
		if err != nil {
			fmt.Println(err)
			panic("Can't create")
		}

		res = "done"
	} else if rq.Args["action"] == "insert" {
		stmt, err := db.Prepare("INSERT INTO `data`(`key`, `val`) VALUES(?,?)")
		if err != nil {
			fmt.Println(err)
			panic("Can't prepare insert")
		}

		_, err = stmt.Exec(rq.Args["key"], rq.Args["val"])
		if err != nil {
			fmt.Println(err)
			panic("Can't insert")
		}

		res = "done"
	} else if rq.Args["action"] == "select" {
		stmt, err := db.Prepare("SELECT `val` FROM `data` WHERE `key` = ?")
		if err != nil {
			fmt.Println(err)
			panic("Can't prepare select")
		}

		rows, err := stmt.Query(rq.Args["key"])
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

	return map[string]string{"res": res}, nil
}
