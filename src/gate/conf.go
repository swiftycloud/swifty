package main

import (
	"fmt"
	"errors"
	"../common"
	"../common/http"
)

type YAMLConfSwd struct {
	Volume		string			`yaml:"volume"`
	Port		int			`yaml:"port"`
	ImgPref		string			`yaml:"img-prefix"`
	Namespace	string			`yaml:"k8s-namespace"`
}

func (cw *YAMLConfSwd)Validate() error {
	if cw.Volume == "" {
		return errors.New("'wdog.volume' not set")
	}
	if cw.Port == 0 {
		return errors.New("'wdog.port' not set")
	}
	if cw.ImgPref == "" {
		cw.ImgPref = "swifty"
		fmt.Printf("'wdog.img-prefix' not set, using default\n")
	}
	if cw.Namespace == "" {
		fmt.Printf("'wdog.k8s-namespace' not set, will us default\n")
	}
	return nil
}

type YAMLConfDaemon struct {
	Addr		string			`yaml:"address"`
	CallGate	string			`yaml:"callgate"`
	LogLevel	string			`yaml:"loglevel"`
	Prometheus	string			`yaml:"prometheus"`
	HTTPS		*xhttp.YAMLConfHTTPS	`yaml:"https,omitempty"`
}

func (cd *YAMLConfDaemon)Validate() error {
	if cd.Addr == "" {
		return errors.New("'daemon.address' not set, want HOST:PORT value")
	}
	if cd.Prometheus == "" {
		return errors.New("'daemon.prometheus' not set, want HOST:PORT value")
	}
	if cd.CallGate == "" {
		fmt.Printf("'daemon.callgate' not set, gate is callgate\n")
	}
	if cd.LogLevel == "" {
		fmt.Printf("'daemon.loglevel' not set, using \"warn\" one\n")
	}
	if cd.HTTPS == nil {
		fmt.Printf("'daemon.https' not set, will try to work over plain http\n")
	}
	return nil
}

type YAMLConfKeystone struct {
	Addr		string			`yaml:"address"`
	Domain		string			`yaml:"domain"`
}

func (ck *YAMLConfKeystone)Validate() error {
	if ck.Addr == "" {
		return errors.New("'keystone.address' not set, want HOST:PORT value")
	}
	if ck.Domain == "" {
		return errors.New("'keystone.domain' not set")
	}
	return nil
}

type YAMLConfRabbit struct {
	Creds		string			`yaml:"creds"`
	AdminPort	string			`yaml:"admport"`
	c		*xh.XCreds
}

type YAMLConfMaria struct {
	Creds		string			`yaml:"creds"`
	QDB		string			`yaml:"quotdb"`
	c		*xh.XCreds
}

type YAMLConfMongo struct {
	Creds		string			`yaml:"creds"`
	c		*xh.XCreds
}

type YAMLConfPostgres struct {
	Creds		string			`yaml:"creds"`
	AdminPort	string			`yaml:"admport"`
	c		*xh.XCreds
}

type YAMLConfS3 struct {
	Creds		string			`yaml:"creds"`
	API		string			`yaml:"api"`
	Notify		string			`yaml:"notify"`
	HiddenKeyTmo	uint32			`yaml:"hidden-key-timeout"`
	c		*xh.XCreds
	cn		*xh.XCreds
}

type YAMLConfMw struct {
	SecKey		string			`yaml:"mwseckey"`
	Rabbit		YAMLConfRabbit		`yaml:"rabbit"`
	Maria		YAMLConfMaria		`yaml:"maria"`
	Mongo		YAMLConfMongo		`yaml:"mongo"`
	Postgres	YAMLConfPostgres	`yaml:"postgres"`
	S3		YAMLConfS3		`yaml:"s3"`
}

func (cm *YAMLConfMw)Validate() error {
	if cm.SecKey == "" {
		return errors.New("'middleware.mwseckey' not set")
	}
	return nil
}

type YAMLConfRange struct {
	Min		uint64			`yaml:"min"`
	Max		uint64			`yaml:"max"`
	Def		uint64			`yaml:"def"`
}

type YAMLConfRt struct {
	Timeout		YAMLConfRange		`yaml:"timeout"`
	Memory		YAMLConfRange		`yaml:"memory"`
	MaxReplicas	uint32			`yaml:"max-replicas"`
}

func (cr *YAMLConfRt)Validate() error {
	if cr.MaxReplicas == 0 {
		cr.MaxReplicas = 8
		fmt.Printf("'runtime.max-replicas' not set, using default 8\n")
	}
	if cr.Timeout.Max == 0 {
		cr.Timeout.Max = 10
		fmt.Printf("'runtime.timeout.max' not set, using default 10sec\n")
	}
	if cr.Timeout.Def == 0 {
		cr.Timeout.Def = 2
		fmt.Printf("'runtime.timeout.def' not set, using default 1sec\n")
	}
	if cr.Memory.Min == 0 {
		cr.Memory.Min = 32
		fmt.Printf("'runtime.memory.min' not set, using default 32m\n")
	}
	if cr.Memory.Max == 0 {
		cr.Memory.Min = 256
		fmt.Printf("'runtime.memory.max' not set, using default 256m\n")
	}
	if cr.Memory.Def == 0 {
		cr.Memory.Def = 64
		fmt.Printf("'runtime.memory.def' not set, using default 64m\n")
	}
	return nil
}

type YAMLConfDemoRepo struct {
	URL		string			`yaml:"url"`
	Functions	map[string]string	`yaml:"functions"`
}

func (dr *YAMLConfDemoRepo)Validate() error {
	if dr.URL == "" {
		return errors.New("'demo-repo.url' not set")
	}
	if len(dr.Functions) == 0 {
		return errors.New("'demo-repo.functions' map not set")
	}
	if _, ok := dr.Functions["user-mgmt"]; !ok {
		fmt.Printf("'demo-repo.functions.user-mgmt' not set, auth-as-a-service will not work")
	}
	return nil
}

type YAMLConf struct {
	Home		string			`yaml:"home"`
	DB		string			`yaml:"db"`
	Daemon		YAMLConfDaemon		`yaml:"daemon"`
	Keystone	YAMLConfKeystone	`yaml:"keystone"`
	Mware		YAMLConfMw		`yaml:"middleware"`
	Runtime		YAMLConfRt		`yaml:"runtime"`
	Wdog		YAMLConfSwd		`yaml:"wdog"`
	RepoSyncRate	int			`yaml:"repo-sync-rate"`
	RepoSyncPeriod	int			`yaml:"repo-sync-period"`
	DemoRepo	YAMLConfDemoRepo	`yaml:"demo-repo"`
}

func (c *YAMLConf)Validate() error {
	err := c.Daemon.Validate()
	if err != nil {
		return err
	}
	err = c.Keystone.Validate()
	if err != nil {
		return err
	}
	err = c.Mware.Validate()
	if err != nil {
		return err
	}
	err = c.Runtime.Validate()
	if err != nil {
		return err
	}
	err = c.Wdog.Validate()
	if err != nil {
		return err
	}
	err = c.DemoRepo.Validate()
	if err != nil {
		return err
	}
	if c.Home == "" {
		return errors.New("'home' not set")
	}
	if c.RepoSyncRate == 0 {
		fmt.Printf("'repo-sync-rate' not set, pulls will be unlimited\n")
	}
	if c.RepoSyncPeriod == 0 {
		fmt.Printf("'repo-sync-period' not set, using default 30min\n")
		c.RepoSyncPeriod = 30
	}
	return nil
}

var conf YAMLConf
