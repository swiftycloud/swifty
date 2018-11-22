package main

import (
	"log"
	"flag"
	"swifty/common"
)

type DBConf struct {
	DB	string	`yaml:"db"`
	Addr	string	`yaml:"address"`
	User	string	`yaml:"user"`
	Pass	string	`yaml:"password"`
}

type Config struct {
	Listen	string	`yaml:"listen"`
	Target	DBConf	`yaml:"target"`
	Modules	map[string]map[string]interface{} `yaml:"modules"`
}

func main() {
	var conf string
	var config Config

	flag.StringVar(&conf, "conf", "/etc/swifty/conf/mongo_proxy.yaml", "Path to config file")
	flag.Parse()

	err := xh.ReadYamlConfig(conf, &config)
	if err != nil {
		log.Printf("Error reading config: %s\n", err.Error())
		return
	}

	for mod, mconf := range config.Modules {
		m, ok := modules[mod]
		if !ok {
			log.Printf("Error: no %s module\n", mod)
			return
		}

		err = m.config(mconf, &config)
		if err != nil {
			log.Printf("Error configuring %s: %s\n", mod, err.Error())
			return
		}

		pipelineAdd(m)
	}

	p := makeProxy(config.Listen, config.Target.Addr)
	if p == nil {
		return
	}

	defer p.Close()

	p.Run()
}
