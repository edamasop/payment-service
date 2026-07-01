package main

import (
	"os"
	"payment-service/internal/app"

	log "github.com/sirupsen/logrus"
)

func main() {
	log.SetOutput(os.Stdout)
	app.Run()
}
