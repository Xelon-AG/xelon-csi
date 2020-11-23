package helper

import (
	"encoding/json"
	"os"
)

type deviceInfo struct {
	Metadata struct {
		LocalVMID string `json:"local_id"`
	} `json:"metadata"`
}

func GetDeviceLocalVMID(metadataFile string) (string, error) {
	f, err := os.Open(metadataFile)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = f.Close()
	}()

	var deviceInfo deviceInfo
	parser := json.NewDecoder(f)
	err = parser.Decode(&deviceInfo)
	return deviceInfo.Metadata.LocalVMID, err
}
