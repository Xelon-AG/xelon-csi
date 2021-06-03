package helper

import (
	"encoding/json"
	"os"
)

type deviceInfo struct {
	Metadata struct {
		LocalVMID string `json:"local_id"`
		Hostname  string `json:"hostname"`
	} `json:"metadata"`
}

func getDeviceInfo(metadataFile string) (*deviceInfo, error) {
	f, err := os.Open(metadataFile)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = f.Close()
	}()

	var deviceInfo deviceInfo
	parser := json.NewDecoder(f)
	err = parser.Decode(&deviceInfo)
	if err != nil {
		return nil, err
	}
	return &deviceInfo, nil
}

func GetDeviceLocalVMID(metadataFile string) (string, error) {
	deviceInfo, err := getDeviceInfo(metadataFile)
	if err != nil {
		return "", err
	}
	return deviceInfo.Metadata.LocalVMID, nil
}

func GetDeviceHostname(metadataFile string) (string, error) {
	deviceInfo, err := getDeviceInfo(metadataFile)
	if err != nil {
		return "", err
	}
	return deviceInfo.Metadata.Hostname, nil
}
