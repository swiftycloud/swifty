package main

import (
	log "github.com/Sirupsen/logrus"

	"os"
)

func main() {
	log.SetLevel(log.DebugLevel)
	log.SetOutput(os.Stdout)
}
