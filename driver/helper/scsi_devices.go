package helper

import (
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
)

const (
	scsiHostPath         = "/sys/class/scsi_host"
	scsiHostScanPath     = "/sys/class/scsi_host/%s/scan"
	scsiDevicePath       = "/sys/class/scsi_device"
	scsiDeviceRescanPath = "/sys/class/scsi_device/%s/device/rescan"
)

func (m *mounter) RescanSCSIDevices() error {
	log := m.log.WithFields(logrus.Fields{
		"method": "rescan_scsi_devices",
	})

	// rescan hosts
	scsiHosts, err := getSCSIHosts()
	if err != nil {
		log.Errorf("could not get scsi hosts, %v", err)
		return err
	}
	for _, scsiHost := range scsiHosts {
		scsiHostScanFile := fmt.Sprintf(scsiHostScanPath, scsiHost)
		log.Debugf("rescan scsi host initiated for %v", scsiHostScanFile)

		if !fileExist(scsiHostScanFile) {
			log.Debugf("scsi host path %v does not exist", scsiHostScanFile)
			continue
		}

		err = os.WriteFile(scsiHostScanFile, []byte("- - -"), 0644)
		if err != nil {
			log.Errorf("could not write to file %v: %v", scsiHostScanFile, err)
		}
	}

	// rescan devices
	scsiDevices, err := getSCSIDevices()
	if err != nil {
		log.Errorf("could not get scsi devices, %v", err)
		return err
	}
	for _, scsiDevice := range scsiDevices {
		scsiDeviceRescanFile := fmt.Sprintf(scsiDeviceRescanPath, scsiDevice)
		log.Debugf("rescan scsi device initiated for %v", scsiDeviceRescanFile)

		if !fileExist(scsiDeviceRescanFile) {
			log.Debugf("scsi device path %v does not exist", scsiDeviceRescanFile)
			continue
		}

		err = os.WriteFile(scsiDeviceRescanFile, []byte("1"), 0644)
		if err != nil {
			log.Errorf("could not write to file %v: %v", scsiDeviceRescanFile, err)
		}
	}

	return nil
}

func getSCSIHosts() ([]string, error) {
	exist := dirExist(scsiHostPath)
	if !exist {
		return nil, fmt.Errorf("directory %v does not exist", scsiHostPath)
	}

	files, err := os.ReadDir(scsiHostPath)
	if err != nil {
		return nil, fmt.Errorf("unable to get list of scsi hosts, %v", err)
	}
	if len(files) == 0 {
		return nil, nil
	}

	var scsiHosts []string
	for _, f := range files {
		scsiHosts = append(scsiHosts, f.Name())
	}
	return scsiHosts, nil
}

func getSCSIDevices() ([]string, error) {
	exist := dirExist(scsiDevicePath)
	if !exist {
		return nil, fmt.Errorf("directory %v does not exist", scsiHostPath)
	}

	files, err := os.ReadDir(scsiDevicePath)
	if err != nil {
		return nil, fmt.Errorf("unable to get list of scsi devices, %v", err)
	}
	if len(files) == 0 {
		return nil, nil
	}

	var scsiDevices []string
	for _, f := range files {
		scsiDevices = append(scsiDevices, f.Name())
	}
	return scsiDevices, nil
}

func dirExist(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return info.IsDir()
}

func fileExist(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}
