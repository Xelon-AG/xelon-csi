package v0

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/sirupsen/logrus"

	driverv1 "github.com/Xelon-AG/xelon-csi/internal/driver"
)

func (d *DriverV0) GetPluginInfo(_ context.Context, _ *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	resp := &csi.GetPluginInfoResponse{
		Name:          driverv1.DefaultDriverName,
		VendorVersion: driverVersion,
	}

	d.log.WithFields(logrus.Fields{
		"response": resp,
		"method":   "get_plugin_info",
	}).Info("get plugin info called")

	return resp, nil
}

func (d *DriverV0) GetPluginCapabilities(_ context.Context, _ *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	resp := &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
		},
	}

	d.log.WithFields(logrus.Fields{
		"response": resp,
		"method":   "get_plugin_capabilities",
	}).Info("get plugin capabilities called")

	return resp, nil
}

// Probe allows to verify that the plugin is in a healthy and ready state
func (d *DriverV0) Probe(_ context.Context, _ *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	d.log.WithFields(logrus.Fields{
		"method": "probe",
	}).Info("probe called")

	return &csi.ProbeResponse{
		Ready: &wrappers.BoolValue{
			Value: true,
		},
	}, nil
}
