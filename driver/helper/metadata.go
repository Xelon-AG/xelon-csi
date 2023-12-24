package helper

import (
	"encoding/json"
	"os"
)

type deviceInfo struct {
	Metadata struct {
		CloudID   string `json:"cloudId"`
		LocalVMID string `json:"local_id"`
		Hostname  string `json:"hostname"`
	} `json:"metadata"`
}

func getDeviceInfo(metadataFile string) (*deviceInfo, error) {
	// reading device info from envvars is the new approach,
	// so try to use it and fallback if no envvar is defined
	deviceInfo := getDeviceInfoFromEnv()
	if deviceInfo.Metadata.LocalVMID == "" {
		return getDeviceInfoFromFile(metadataFile)
	}
	return deviceInfo, nil
}

func getDeviceInfoFromEnv() *deviceInfo {
	cloudID := os.Getenv("XELON_CLOUD_ID")
	localVMID := os.Getenv("XELON_LOCAL_VM_ID")
	hostname := os.Getenv("XELON_VM_HOSTNAME")

	deviceInfoFromEnv := new(deviceInfo)
	deviceInfoFromEnv.Metadata.CloudID = cloudID
	deviceInfoFromEnv.Metadata.LocalVMID = localVMID
	deviceInfoFromEnv.Metadata.Hostname = hostname

	return deviceInfoFromEnv
}

func getDeviceInfoFromFile(metadataFile string) (*deviceInfo, error) {
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

func GetDeviceCloudID(metadataFile string) (string, error) {
	deviceInfo, err := getDeviceInfo(metadataFile)
	if err != nil {
		return "", err
	}
	return deviceInfo.Metadata.CloudID, nil
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
