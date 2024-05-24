package v0

import (
	"log"
	"time"

	"github.com/sirupsen/logrus"

	driverv1 "github.com/Xelon-AG/xelon-csi/internal/driver"
	"github.com/Xelon-AG/xelon-csi/internal/driver/v0/helper"
)

func InitializeLogging(logLevel, mode, metadataFile string) *logrus.Entry {
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
		"component": driverv1.DefaultDriverName,
		"cloud_id":  cloudID,
		"device":    localVMID,
		"service":   mode,
		"version":   GetVersion().DriverVersion,
	})
}
