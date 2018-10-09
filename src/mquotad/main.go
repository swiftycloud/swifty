package main

import (
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
	"flag"
	"time"
	"os"
	"swifty/common"
	"swifty/common/secrets"
)

var zcfg zap.Config = zap.Config {
	Level:            zap.NewAtomicLevelAt(zap.DebugLevel),
	Development:      true,
	DisableStacktrace:true,
	Encoding:         "console",
	EncoderConfig:    zap.NewDevelopmentEncoderConfig(),
	OutputPaths:      []string{"stderr"},
	ErrorOutputPaths: []string{"stderr"},
}
var logger, _ = zcfg.Build()
var log = logger.Sugar()

type YAMLConf struct {
	DB		string		`yaml:"db"`
	Addr		string		`yaml:"address"`
	User		string		`yaml:"user"`
	Pass		string		`yaml:"password"`
}

var conf YAMLConf
var qdSecrets map[string]string
const quotaOverflowReq = `
SELECT * FROM (
	SELECT 
		information_schema.tables.table_schema, 
		quotas.rows as rowsl, 
		SUM(information_schema.tables.table_rows) as rows, 
		quotas.size as sizel, 
		SUM(information_schema.tables.data_length + information_schema.tables.index_length) as size, 
		quotas.locked 
	FROM information_schema.tables
	JOIN quotas ON quotas.id=information_schema.tables.table_schema
	GROUP BY information_schema.tables.table_schema
) ifo
WHERE
	((ifo.rows > ifo.rowsl OR ifo.size > ifo.sizel) AND locked=false)
	OR
	((ifo.rows < ifo.rowsl AND ifo.size < ifo.sizel) AND locked=true)
`

func lockAccess(qdb *sql.DB, id string) {
	/* FIXME -- revoke ins and upd privs for db id from user id (swifty makes them match) */
}

func unlockAccess(qdb *sql.DB, id string) {
	/* FIXME -- grant all privs back */
}

func checkQuotas(qdb *sql.DB, delay *uint) {
	rows, err := qdb.Query(quotaOverflowReq)
	if err != nil {
		log.Debugf("Can't get quota status: %s", err.Error())
		return
	}
	defer rows.Close()

	for rows.Next() {
		var swid string
		var tqsize uint
		var tqrows uint
		var tsize uint
		var trows uint
		var locked bool

		err = rows.Scan(&swid, &tqsize, &tsize, &tqrows, &trows, &locked)
		if err != nil {
			log.Debugf("Can't scan row: %s", err.Error())
			continue
		}

		if locked {
			log.Debugf("Unlock DB %s: %d/%d %d/%d", swid, tsize, tqsize, trows, tqrows)
			unlockAccess(qdb, swid)
		} else {
			log.Debugf("Lock DB %s: %d/%d %d/%d", swid, tsize, tqsize, trows, tqrows)
			lockAccess(qdb, swid)
		}
	}
}

func main() {
	var conf_path string
	var err error

	flag.StringVar(&conf_path,
			"conf",
				"/etc/swifty/conf/mquotad.yaml",
				"path to the configuration file")
	flag.Parse()
	if _, err := os.Stat(conf_path); err == nil {
		err = xh.ReadYamlConfig(conf_path, &conf)
	}
	if err != nil {
		log.Errorf("Can't read config: %s", err.Error())
		return
	}

	log.Debugf("Config: %v", conf)

	qdSecrets, err = xsecret.ReadSecrets("mqd")
	if err != nil {
		log.Errorf("Can't read gate secrets: %s", err.Error())
		return
	}

	qdb, err := sql.Open("mysql", conf.User + ":" + qdSecrets[conf.Pass] + "@tcp(" + conf.Addr + ")/" + conf.DB)
	if err == nil {
		err = qdb.Ping()
	}
	if err != nil {
		log.Errorf("Can't connect to maria (%s)", err.Error())
		return
	}

	delay := uint(10)
	for {
		log.Debugf("Next scan in %d seconds", delay)
		time.Sleep(time.Duration(delay) * time.Second)
		checkQuotas(qdb, &delay)
	}
}
