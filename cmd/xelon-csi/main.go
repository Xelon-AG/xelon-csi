package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/Xelon-AG/xelon-csi/driver"
	"github.com/Xelon-AG/xelon-csi/driver/helper"
)

func main() {
	var (
		apiURL         = flag.String("api-url", "https://vdc.xelon.ch/api/service/", "Xelon API URL")
		clientID       = flag.String("client-id", "", "Xelon client id for IP ranges")
		endpoint       = flag.String("endpoint", "unix:///var/lib/kubelet/plugins/"+driver.DefaultDriverName+"/csi.sock", "CSI endpoint")
		logLevel       = flag.String("log-level", "info", "The log level for the CSI driver")
		metadataFile   = flag.String("metadata-file", "/etc/init.d/metadata.json", "The path to the metadata file on Xelon devices")
		mode           = flag.String("mode", string(driver.AllMode), "The mode in which the CSI driver will be run (all, node, controller)")
		rescanOnResize = flag.Bool("rescan-on-resize", true, "Rescan block device and verify its size before expanding the filesystem (node mode)")
		token          = flag.String("token", "", "Xelon access token")
		version        = flag.Bool("version", false, "Print the version and exit.")
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
			BaseURL:        *apiURL,
			ClientID:       *clientID,
			Endpoint:       *endpoint,
			Mode:           driver.Mode(*mode),
			MetadataFile:   *metadataFile,
			RescanOnResize: *rescanOnResize,
			Token:          *token,
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

	cloudID, err := helper.GetDeviceCloudID(metadataFile)
	if err != nil {
		log.Printf("Couldn't get cloudID from Xelon device (use 1 as default), %v\n", err)
		cloudID = "1"
	}

	return logger.WithFields(logrus.Fields{
		"component": driver.DefaultDriverName,
		"cloud_id":  cloudID,
		"device":    localVMID,
		"service":   mode,
		"version":   driver.GetVersion().DriverVersion,
	})
}
