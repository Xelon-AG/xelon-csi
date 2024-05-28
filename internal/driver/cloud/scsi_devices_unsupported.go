//go:build !linux

package cloud

import "k8s.io/klog/v2"

func RescanSCSIDevices() error {
	klog.V(2).InfoS("Cannot rescan SCSI devices because it is not supported for this build",
		"method", "RescanSCSIDevices",
	)
	return nil
}
