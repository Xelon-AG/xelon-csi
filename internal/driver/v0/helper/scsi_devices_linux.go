//go:build linux

package helper

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/unix"

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
		scsiHostScanFile, err := filepath.EvalSymlinks(fmt.Sprintf(scsiHostScanPath, scsiHost))
		if err != nil {
			log.Errorf("could not evaluate symlinks: %v", err)
		}

		log.Debugf("rescan scsi host initiated for %v", scsiHostScanFile)

		if !fileExist(scsiHostScanFile) {
			log.Debugf("scsi host path %v does not exist", scsiHostScanFile)
			continue
		}

		err = os.WriteFile(scsiHostScanFile, []byte("- - -"), 0666)
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
		scsiDeviceRescanFile, err := filepath.EvalSymlinks(fmt.Sprintf(scsiDeviceRescanPath, scsiDevice))
		if err != nil {
			log.Errorf("could not evaluate symlinks: %v", err)
		}

		log.Debugf("rescan scsi device initiated for %v", scsiDeviceRescanFile)

		if !fileExist(scsiDeviceRescanFile) {
			log.Debugf("scsi device path %v does not exist", scsiDeviceRescanFile)
			continue
		}

		err = os.WriteFile(scsiDeviceRescanFile, []byte("1"), 0666)
		if err != nil {
			log.Errorf("could not write to file %v: %v", scsiDeviceRescanFile, err)
		}
	}

	// inform about partition table changes
	// this command will always be executed last!
	partprobeCmd := "partprobe"
	partprobeArgs := "-s"
	_, err = exec.LookPath(partprobeCmd)
	if err != nil {
		if err == exec.ErrNotFound {
			log.Warnf("%q executable not found in $PATH, skip informing about partition table changes", partprobeCmd)
			return nil
		}
	}
	log.WithFields(logrus.Fields{
		"cmd":  partprobeCmd,
		"args": partprobeArgs,
	}).Debug("executing partprobe command")
	out, err := exec.Command(partprobeCmd, partprobeArgs).CombinedOutput()
	if err != nil {
		log.Errorf("informing about partition table changes failed: %v; output: %q", err, string(out))
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

func (m *mounter) GetVolumeStatistics(volumePath string) (VolumeStatistics, error) {
	fs := &unix.Statfs_t{}
	err := unix.Statfs(volumePath, fs)
	if err != nil {
		return VolumeStatistics{}, err
	}

	totalBytes := fs.Blocks * uint64(fs.Bsize)
	availableBytes := fs.Bfree * uint64(fs.Bsize)
	usedBytes := totalBytes - availableBytes

	totalInodes := fs.Files
	availableInodes := fs.Ffree
	usedInodes := totalInodes - availableInodes

	return VolumeStatistics{
		AvailableBytes:  int64(availableBytes),
		AvailableInodes: int64(availableInodes),
		TotalBytes:      int64(totalBytes),
		TotalInodes:     int64(totalInodes),
		UsedBytes:       int64(usedBytes),
		UsedInodes:      int64(usedInodes),
	}, nil
}
