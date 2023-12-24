package helper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetDeviceInfoFromEnv(t *testing.T) {
	setUpEnvVars()
	_ = os.Setenv("XELON_CLOUD_ID", "5")
	_ = os.Setenv("XELON_LOCAL_VM_ID", "abcd1234")
	_ = os.Setenv("XELON_VM_HOSTNAME", "test.local")

	deviceInfo := getDeviceInfoFromEnv()

	assert.Equal(t, "5", deviceInfo.Metadata.CloudID)
	assert.Equal(t, "abcd1234", deviceInfo.Metadata.LocalVMID)
	assert.Equal(t, "test.local", deviceInfo.Metadata.Hostname)
}

func TestGetDeviceInfoFromFile_valid(t *testing.T) {
	setUpEnvVars()
	metadataFile := filepath.Join("testdata", "valid-metadata.json")

	deviceInfo, err := getDeviceInfoFromFile(metadataFile)

	assert.NoError(t, err)
	assert.Equal(t, "1", deviceInfo.Metadata.CloudID)
	assert.Equal(t, "abcd1234", deviceInfo.Metadata.LocalVMID)
	assert.Equal(t, "test.local", deviceInfo.Metadata.Hostname)
}

func TestGetDeviceInfoFromFile_invalid(t *testing.T) {
	setUpEnvVars()
	metadataFile := filepath.Join("testdata", "invalid-metadata.json")

	_, err := getDeviceInfoFromFile(metadataFile)

	assert.Error(t, err)
}

func TestGetDeviceInfo(t *testing.T) {
	setUpEnvVars()
	// XELON_LOCAL_VM_ID is not defined, fallback to file values
	_ = os.Setenv("XELON_CLOUD_ID", "5")
	metadataFile := filepath.Join("testdata", "valid-metadata.json")

	deviceInfo, err := getDeviceInfo(metadataFile)

	assert.NoError(t, err)
	assert.Equal(t, "1", deviceInfo.Metadata.CloudID)
}

func setUpEnvVars() {
	_ = os.Unsetenv("XELON_CLOUD_ID")
	_ = os.Unsetenv("XELON_LOCAL_VM_ID")
	_ = os.Unsetenv("XELON_VM_HOSTNAME")
}
