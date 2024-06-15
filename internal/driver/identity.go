package driver

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"k8s.io/klog/v2"
)

func (d *Driver) GetPluginInfo(_ context.Context, req *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	klog.V(5).InfoS("Get plugin information", "method", "GetPluginInfo", "req", *req)

	return &csi.GetPluginInfoResponse{
		Name:          DefaultDriverName,
		VendorVersion: GetVersion(),
	}, nil
}

func (d *Driver) GetPluginCapabilities(_ context.Context, req *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	klog.V(5).InfoS("Get plugin capabilities", "method", "GetPluginCapabilities", "req", *req)

	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{{
			Type: &csi.PluginCapability_Service_{
				Service: &csi.PluginCapability_Service{
					Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
				},
			},
		}},
	}, nil
}

func (d *Driver) Probe(_ context.Context, req *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	klog.V(5).InfoS("Call probe", "method", "Probe", "req", *req)

	return &csi.ProbeResponse{}, nil
}
