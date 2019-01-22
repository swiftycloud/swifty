/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"log"
	"flag"
	"swifty/common"
	"swifty/common/tcproxy"
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

	flag.StringVar(&conf, "conf", "/etc/swifty/conf/maria_proxy.yaml", "Path to config file")
	flag.Parse()

	err := xh.ReadYamlConfig(conf, &config)
	if err != nil {
		log.Printf("Error reading config: %s\n", err.Error())
		return
	}

	err = configureSession(&config)
	if err != nil {
		log.Printf("Error configuring session: %s\n", err.Error())
		return
	}

	err = loadModules(&config)
	if err != nil {
		log.Printf("Error loading modules: %s\n", err.Error())
		return
	}

	p := tcproxy.MakeProxy(config.Listen, config.Target.Addr, &mariaConsumer{})
	if p == nil {
		return
	}

	defer p.Close()

	p.Run()
}
