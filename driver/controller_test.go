package driver

//
// import (
// 	"testing"
//
// 	"github.com/container-storage-interface/spec/lib/go/csi"
// 	"github.com/stretchr/testify/assert"
// )
//
// func TestController_validateVolumeCapabilities(t *testing.T) {
// 	type testCase struct {
// 		input    []*csi.VolumeCapability
// 		expected bool
// 	}
// 	tests := map[string]testCase{
// 		"nil_capabilities": {
// 			input:    nil,
// 			expected: true,
// 		},
// 		"nil_capability": {
// 			input:    []*csi.VolumeCapability{nil},
// 			expected: false,
// 		},
// 		"nil_access_mode": {
// 			input: []*csi.VolumeCapability{{
// 				AccessMode: nil,
// 			}},
// 			expected: false,
// 		},
// 		"multi_node_multi_writer": {
// 			input: []*csi.VolumeCapability{{
// 				AccessMode: &csi.VolumeCapability_AccessMode{
// 					Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
// 				}},
// 			},
// 			expected: false,
// 		},
// 		"multi_node_single_writer": {
// 			input: []*csi.VolumeCapability{{
// 				AccessMode: &csi.VolumeCapability_AccessMode{
// 					Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER,
// 				}},
// 			},
// 			expected: false,
// 		},
// 		"multi_node_reader_only": {
// 			input: []*csi.VolumeCapability{{
// 				AccessMode: &csi.VolumeCapability_AccessMode{
// 					Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
// 				}},
// 			},
// 			expected: false,
// 		},
// 		"single_node_writer": {
// 			input: []*csi.VolumeCapability{{
// 				AccessMode: &csi.VolumeCapability_AccessMode{
// 					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
// 				}},
// 			},
// 			expected: true,
// 		},
// 		"single_node_reader_only": {
// 			input: []*csi.VolumeCapability{{
// 				AccessMode: &csi.VolumeCapability_AccessMode{
// 					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
// 				}},
// 			},
// 			expected: false,
// 		},
// 	}
//
// 	for name, test := range tests {
// 		t.Run(name, func(t *testing.T) {
// 			actual := isValidCapabilities(test.input)
// 			assert.Equal(t, test.expected, actual)
// 		})
// 	}
// }
