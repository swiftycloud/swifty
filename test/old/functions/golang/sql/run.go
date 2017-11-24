package main

import (
	"fmt"
	"os"
	"strings"
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
)

func main() {
	mwn := strings.ToUpper(os.Args[2])
	addr := os.Getenv("MWARE_" + mwn + "_ADDR")
	user := os.Getenv("MWARE_" + mwn + "_USER")
	pass := os.Getenv("MWARE_" + mwn + "_PASS")
	dbn := os.Getenv("MWARE_" + mwn + "_DBNAME")

	db, err := sql.Open("mysql", user + ":" + pass + "@tcp(" + addr + ")/" + dbn)
	if err != nil {
		fmt.Println(err)
		panic("Can't open DB")
	}
	defer db.Close()

	err = db.Ping()
	if err != nil {
		fmt.Println(err)
		panic("Can't ping DB")
	}

	if os.Args[1] == "create" {
		_, err = db.Exec("CREATE TABLE `foo` (`id` int(11) NOT NULL AUTO_INCREMENT, " +
					"`name` varchar(255), PRIMARY KEY(`id`))")
		if err != nil {
			fmt.Println(err)
			panic("Can't insert")
		}
	} else if os.Args[1] == "insert" {
		stmt, err := db.Prepare("INSERT INTO `foo`(`name`) VALUES(?)")
		if err != nil {
			fmt.Println(err)
			panic("Can't prepare stmt")
		}

		_, err = stmt.Exec(os.Args[3])
		if err != nil {
			fmt.Println(err)
			panic("Can't insert")
		}
	} else if os.Args[1] == "select" {
		rows, err := db.Query("SELECT * FROM foo")
		if err != nil {
			fmt.Println(err)
			panic("Can't query foo")
		}
		defer rows.Close()

		for rows.Next() {
			var id int
			var name string

			err = rows.Scan(&id, &name)
			if err != nil {
				panic("No rows")
			}

			fmt.Println("golang:sql:" + name)
		}
	}
}
