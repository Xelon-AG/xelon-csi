package internal

import (
	"encoding/json"
	"os"
)

type nodeInfo struct {
	Metadata struct {
		LocalVMID string `json:"local_id"`
	} `json:"metadata"`
}

func GetNodeLocalVMID(metadataFile string) (string, error) {
	f, err := os.Open(metadataFile)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = f.Close()
	}()

	var nodeInfo nodeInfo
	parser := json.NewDecoder(f)
	err = parser.Decode(&nodeInfo)
	return nodeInfo.Metadata.LocalVMID, err
}
