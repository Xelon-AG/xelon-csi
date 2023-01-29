//go:build !linux

package helper

import "github.com/sirupsen/logrus"

func (m *mounter) RescanSCSIDevices() error {
	m.log.WithFields(logrus.Fields{
		"method": "rescan_scsi_devices",
	}).Info("RescanSCSIDevices is not supported for this build")
	return nil
}
