//go:build !linux

package helper

import "github.com/sirupsen/logrus"

func (m *mounter) RescanSCSIDevices() error {
	m.log.WithFields(logrus.Fields{
		"method": "rescan_scsi_devices",
	}).Info("RescanSCSIDevices is not supported for this build")
	return nil
}

func (m *mounter) GetVolumeStatistics(_ string) (VolumeStatistics, error) {
	m.log.WithFields(logrus.Fields{
		"method": "get_volume_statistics",
	}).Info("GetVolumeStatistics is not supported for this build")
	return VolumeStatistics{}, nil
}
