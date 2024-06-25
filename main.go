package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	flag.Parse()

	stopch := make(chan struct{})
	app, err := NewApplication(*url, *logFile, *dbfile, *keyCount)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	app.Logger.Println("Starting nomenclator")
	go app.Run(stopch)
	<-stopch
	app.Logger.Println("Stopping nomenclator")
	fmt.Println("Complete")

}
