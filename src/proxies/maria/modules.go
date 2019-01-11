/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"fmt"
	"log"
)

type module interface {
	request(string, *maria_req) error
	config(map[string]interface{}) error
}

var modules map[string]module = map[string]module {
	"show":	&rqShow{},
}

func loadModules(config *Config) error {
	for mod, mconf := range config.Modules {
		m, ok := modules[mod]
		if !ok {
			return fmt.Errorf("Error: no %s module\n", mod)
		}

		err := m.config(mconf)
		if err != nil {
			return fmt.Errorf("Error configuring %s: %s\n", mod, err.Error())
		}

		log.Printf("+%s\n", mod)
		pipelineAdd(m)
	}

	return nil
}
