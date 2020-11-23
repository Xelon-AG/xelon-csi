package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Xelon-AG/xelon-csi/driver"
	"github.com/Xelon-AG/xelon-csi/driver/helper"
	"github.com/sirupsen/logrus"
)

func main() {
	var (
		apiURL       = flag.String("api-url", "https://vdc.xelon.ch/api/service/", "Xelon API URL")
		endpoint     = flag.String("endpoint", "unix:///var/lib/kubelet/plugins/"+driver.DefaultDriverName+"/csi.sock", "CSI endpoint")
		logLevel     = flag.String("log-level", "info", "The log level for the CSI driver")
		mode         = flag.String("mode", string(driver.AllMode), "The mode in which the CSI driver will be run (all, node, controller)")
		metadataFile = flag.String("metadata-file", "/etc/init.d/metadata.json", "The path to the metadata file on Xelon devices")
		token        = flag.String("token", "", "Xelon access token")
		version      = flag.Bool("version", false, "Print the version and exit.")
	)
	flag.Parse()

	if *version {
		info := driver.GetVersion()
		fmt.Println("Xelon Persistent Storage CSI Driver")
		fmt.Printf(" Version:      %s\n", info.DriverVersion)
		fmt.Printf(" Built:        %s\n", info.BuildDate)
		fmt.Printf(" Git commit:   %s\n", info.GitCommit)
		fmt.Printf(" Git state:    %s\n", info.GitTreeState)
		fmt.Printf(" Go version:   %s\n", info.GoVersion)
		fmt.Printf(" OS/Arch:      %s\n", info.Platform)
		os.Exit(0)
	}

	logger := initializeLogging(*logLevel, *mode, *metadataFile)
	d, err := driver.NewDriver(
		&driver.Config{
			BaseURL:      *apiURL,
			Endpoint:     *endpoint,
			Mode:         driver.Mode(*mode),
			MetadataFile: *metadataFile,
			Token:        *token,
		},
		logger)
	if err != nil {
		logger.Fatalln(err)
	}

	if err := d.Run(); err != nil {
		logger.Fatalln(err)
	}
}

func initializeLogging(logLevel, mode, metadataFile string) *logrus.Entry {
	var logger = logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
	})

	switch logLevel {
	case "", "info":
		logger.SetLevel(logrus.InfoLevel)
	case "debug":
		logger.SetLevel(logrus.DebugLevel)
	case "warn":
		logger.SetLevel(logrus.WarnLevel)
	}

	localVMID, err := helper.GetDeviceLocalVMID(metadataFile)
	if err != nil {
		log.Printf("Couldn't get localVMID from Xelon device, %v\n", err)
		localVMID = "unknown"
	}

	return logger.WithFields(logrus.Fields{
		"component": driver.DefaultDriverName,
		"device":    localVMID,
		"service":   mode,
		"version":   driver.GetVersion().DriverVersion,
	})
}
