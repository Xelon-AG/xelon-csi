package driver

import (
	"context"
	"fmt"

	"github.com/Xelon-AG/xelon-sdk-go/xelon"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog"
)

var (
	// controllerCapabilities represents the capabilities of the Xelon Volumes
	controllerCapabilities = []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
	}
)

type controllerService struct {
	xelon    *xelon.Client
	tenantID string

	// mux sync.Mutex
}

func (d *Driver) initializeControllerService() error {
	d.log.Info("Initializing Xelon controller service")

	userAgent := fmt.Sprintf("%s/%s (%s)", DefaultDriverName, driverVersion, gitCommit)

	client := xelon.NewClient(d.config.Token)
	client.SetBaseURL(d.config.BaseURL)
	client.SetUserAgent(userAgent)

	tenant, _, err := client.Tenant.Get(context.Background())
	if err != nil {
		return err
	}

	d.log.Debugf("Tenant ID: %s", tenant.TenantID)

	d.controllerService = &controllerService{
		xelon:    client,
		tenantID: tenant.TenantID,
	}

	return nil
}

// CreateVolume creates a new volume with the given CreateVolumeRequest.
func (d *Driver) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	klog.V(4).Infof("CreateVolume called with %v", *req)

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "Name must be provided")
	}

	// validation

	volumeName := req.Name

	storages, _, err := d.xelon.PersistentStorage.List(ctx, d.tenantID)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	for _, storage := range storages {
		if storage.Name == volumeName {
			// volume already exists
			return &csi.CreateVolumeResponse{
				Volume: &csi.Volume{
					VolumeId:      storage.LocalID,
					CapacityBytes: int64(storage.Capacity),
				},
			}, nil
		}
	}

	// creating volume via Xelon API

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId: "123456-abc",
		},
	}, nil
}

func (d *Driver) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	klog.Infof("DeleteVolume called")
	return &csi.DeleteVolumeResponse{}, nil
}

func (d *Driver) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	klog.V(4).Infof("ControllerPublishVolume is not yet implemented")
	return nil, status.Error(codes.Unimplemented, "ControllerPublishVolume is not yet implemented")
}

// ControllerUnpublishVolume de-attaches the given volume from the node
func (d *Driver) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	klog.V(4).Infof("ControllerUnpublishVolume is not yet implemented")
	return nil, status.Error(codes.Unimplemented, "ControllerUnpublishVolume is not yet implemented")
}

func (d *Driver) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	klog.V(4).Infof("ValidateVolumeCapabilities is not yet implemented")
	return nil, status.Error(codes.Unimplemented, "ValidateVolumeCapabilities is not yet implemented")
}

func (d *Driver) ListVolumes(_ context.Context, _ *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	klog.V(4).Infof("ListVolumes is not yet implemented")
	return nil, status.Error(codes.Unimplemented, "")
}

func (d *Driver) GetCapacity(_ context.Context, _ *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	klog.V(4).Infof("GetCapacity is not yet implemented")
	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerGetCapabilities get capabilities of the Xelon controller.
func (d *Driver) ControllerGetCapabilities(_ context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	klog.V(4).Infof("ControllerGetCapabilities called with %v", *req)

	var capabilities []*csi.ControllerServiceCapability
	for _, capability := range controllerCapabilities {
		capabilities = append(capabilities, &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: capability,
				},
			},
		})
	}

	return &csi.ControllerGetCapabilitiesResponse{Capabilities: capabilities}, nil
}

// CreateSnapshot will be called by the CO to create a new snapshot from a
// source volume on behalf of a user.
func (d *Driver) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	klog.V(4).Infof("CreateSnapshot is not yet implemented")
	return nil, status.Error(codes.Unimplemented, "CreateSnapshot is not yet implemented")
}

// DeleteSnapshot will be called by the CO to delete a snapshot.
func (d *Driver) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	klog.V(4).Infof("DeleteSnapshot is not yet implemented")
	return nil, status.Error(codes.Unimplemented, "DeleteSnapshot is not yet implemented")
}

// ListSnapshots returns the information about all snapshots on the storage
// system within the given parameters regardless of how they were created.
func (d *Driver) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	klog.V(4).Infof("ListSnapshots is not yet implemented")
	return nil, status.Error(codes.Unimplemented, "ListSnapshots is not yet implemented")
}

// ControllerExpandVolume is called from the resizer to increase the volume size.
func (d *Driver) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	klog.V(4).Infof("ControllerExpandVolume is not yet implemented")
	return nil, status.Error(codes.Unimplemented, "ControllerExpandVolume is not yet implemented")
}

// ControllerGetVolume gets a specific volume.
func (d *Driver) ControllerGetVolume(ctx context.Context, req *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	klog.V(4).Infof("ControllerGetVolume is not yet implemented")
	return nil, status.Error(codes.Unimplemented, "ControllerGetVolume is not yet implemented")
}
