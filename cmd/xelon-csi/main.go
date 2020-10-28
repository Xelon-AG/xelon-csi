package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Xelon-AG/xelon-csi/driver"
)

func main() {
	var (
		token      = flag.String("token", "", "Xelon access token")
		controller = flag.Bool("controller", false, "")
		// url        = flag.String("api-url", "https://vdcnew.xelon.ch/api/service/", "Xelon API URL")
		driverName = flag.String("driver-name", driver.DefaultDriverName, "Name for the driver")
		version    = flag.Bool("version", false, "Print the version and exit.")
	)
	flag.Parse()

	if *version {
		fmt.Printf("%s - %s\n", "dev", "unknown")
		os.Exit(0)
	}

	drv, err := driver.NewDriver(*token, *driverName, *controller)
	if err != nil {
		log.Fatalln(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-c
		cancel()
	}()

	if err := drv.Run(ctx); err != nil {
		log.Fatalln(err)
	}
}
