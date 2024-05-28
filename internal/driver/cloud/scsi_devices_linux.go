//go:build linux

package cloud

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"k8s.io/klog/v2"
)

const (
	scsiHostPath         = "/sys/class/scsi_host"
	scsiHostScanPath     = "/sys/class/scsi_host/%s/scan"
	scsiDevicePath       = "/sys/class/scsi_device"
	scsiDeviceRescanPath = "/sys/class/scsi_device/%s/device/rescan"
)

func RescanSCSIDevices() error {
	// rescan hosts
	klog.V(5).InfoS("Attempting to rescan scsi hosts",
		"method", "RescanSCSIDevices",
	)
	scsiHosts, err := getSCSIHosts()
	if err != nil {
		return fmt.Errorf("could not get scsi hosts, %v", err)
	}
	for _, scsiHost := range scsiHosts {
		scsiHostScanFile, err := filepath.EvalSymlinks(fmt.Sprintf(scsiHostScanPath, scsiHost))
		if err != nil {
			klog.ErrorS(err, "Failed to evaluate symlinks")
		}

		if !fileExist(scsiHostScanFile) {
			klog.V(5).InfoS("Skip rescanning because scsi host path does not exist",
				"method", "RescanSCSIDevices",
				"scsi_host_path", scsiHostScanFile,
			)
			continue
		}

		klog.V(5).InfoS("Initiate scsi host rescan",
			"method", "RescanSCSIDevices",
			"scsi_host_path", scsiHostScanFile,
		)
		err = os.WriteFile(scsiHostScanFile, []byte("- - -"), 0666)
		if err != nil {
			klog.ErrorS(err, "Failed to write to scsi host file")
		}
	}

	// rescan devices
	klog.V(5).InfoS("Attempting to rescan scsi devices",
		"method", "RescanSCSIDevices",
	)
	scsiDevices, err := getSCSIDevices()
	if err != nil {
		return fmt.Errorf("could not get scsi devices, %v", err)
	}
	for _, scsiDevice := range scsiDevices {
		scsiDeviceRescanFile, err := filepath.EvalSymlinks(fmt.Sprintf(scsiDeviceRescanPath, scsiDevice))
		if err != nil {
			klog.ErrorS(err, "Failed to evaluate symlinks")
		}

		if !fileExist(scsiDeviceRescanFile) {
			klog.V(5).InfoS("Skip rescanning because scsi device path does not exist",
				"method", "RescanSCSIDevices",
				"scsi_device_path", scsiDeviceRescanFile,
			)
			continue
		}

		klog.V(5).InfoS("Initiate scsi device rescan",
			"method", "RescanSCSIDevices",
			"scsi_device_path", scsiDeviceRescanFile,
		)
		err = os.WriteFile(scsiDeviceRescanFile, []byte("1"), 0666)
		if err != nil {
			klog.ErrorS(err, "Failed to write to scsi device file")
		}
	}

	// inform about partition table changes
	// this command will always be executed last!
	partprobeCmd := "partprobe"
	_, err = exec.LookPath(partprobeCmd)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			klog.V(2).InfoS("Skip informing about partition table changes, because partprobe not found in PATH",
				"method", "RescanSCSIDevices",
			)
			return nil
		}
	}
	klog.V(5).InfoS("Informing about partition table changes with partprobe command",
		"method", "RescanSCSIDevices",
	)
	out, err := exec.Command(partprobeCmd, "-s").CombinedOutput()
	if err != nil {
		klog.ErrorS(err, "Failed to inform about partition table changes",
			"method", "RescanSCSIDevices",
			"command_output", string(out),
		)
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
