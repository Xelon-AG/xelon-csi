package driver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	"github.com/Xelon-AG/xelon-csi/internal/driver/cloud"
	"github.com/Xelon-AG/xelon-sdk-go/xelon"
)

const (
	_   = iota
	kiB = 1 << (10 * iota)
	miB
	giB
	tiB

	minVolumeSizeInBytes     int64 = 5 * giB
	defaultVolumeSizeInBytes int64 = 10 * giB

	volumeStatusCheckInterval = 10 * time.Second
	volumeStatusCheckTimeout  = 300 * time.Second

	xelonStorageUUID = DefaultDriverName + "/storage-uuid"
	xelonStorageName = DefaultDriverName + "/storage-name"
)

var (
	controllerCapabilities = []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
	}

	// Xelon currently only support a single volume to be attached to a single node
	// in read/write mode. This corresponds to `accessModes.ReadWriteOnce` in a
	// PVC resource on Kubernetes
	supportedAccessMode = &csi.VolumeCapability_AccessMode{
		Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
	}
)

type controllerService struct {
	xelon *xelon.Client

	cloudID  string
	tenantID string
}

func newControllerService(ctx context.Context, opts *Options) (*controllerService, error) {
	klog.V(2).InfoS("Initialize controller service")

	xelonClient, err := cloud.NewXelonClient(opts.XelonToken, opts.XelonClientID, opts.XelonBaseURL, UserAgent())
	if err != nil {
		return nil, err
	}

	controllerService := &controllerService{
		xelon: xelonClient,
	}

	tenant, _, err := xelonClient.Tenants.GetCurrent(ctx)
	if err != nil {
		return nil, err
	}
	klog.V(5).InfoS("Fetched info about tenant", "tenant_id", tenant.TenantID)
	controllerService.tenantID = tenant.TenantID

	klog.V(5).InfoS("Verifying that tenant has an access to the cloud", "tenant_id", tenant.TenantID, "cloud_id", opts.XelonCloudID)
	hvs, _, err := xelonClient.Clouds.List(ctx, tenant.TenantID)
	if err != nil {
		return nil, err
	}
	for _, hv := range hvs {
		if strconv.Itoa(hv.ID) == opts.XelonCloudID {
			controllerService.cloudID = opts.XelonCloudID
			return controllerService, nil
		}
	}

	return nil, errors.New("tenant has no access to the specified cloud")
}

func (d *Driver) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "volume name not provided")
	}
	if len(req.VolumeCapabilities) == 0 {
		return nil, status.Error(codes.InvalidArgument, "volume capabilities not provided")
	}

	size, err := extractStorage(req.CapacityRange)
	if err != nil {
		return nil, status.Errorf(codes.OutOfRange, "invalid capacity range: %v", err)
	}

	volumeName := req.Name

	klog.V(2).InfoS("Creating new volume",
		"method", "CreateVolume",
		"storage_size_gigabytes", size/giB,
		"volume_capabilities", req.VolumeCapabilities,
		"volume_name", volumeName,
	)

	klog.V(5).InfoS("Fetching persistent storage by name",
		"method", "CreateVolume",
		"tenant_id", d.tenantID,
		"volume_name", volumeName,
	)
	storage, response, err := d.xelon.PersistentStorages.GetByName(ctx, d.tenantID, volumeName)
	if err != nil && response != nil && response.StatusCode != http.StatusNotFound {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if storage != nil {
		if storage.UUID != "" && storage.Formatted == 1 {
			klog.V(2).InfoS("Volume already created",
				"method", "CreateVolume",
				"volume_name", volumeName,
			)
			return &csi.CreateVolumeResponse{
				Volume: &csi.Volume{
					VolumeId:      storage.LocalID,
					CapacityBytes: int64(storage.Capacity * giB),
				},
			}, nil
		} else {
			klog.V(2).InfoS("Volume is still creating",
				"method", "CreateVolume",
				"volume_id", storage.LocalID,
				"volume_name", volumeName,
			)
			return nil, status.Errorf(codes.AlreadyExists, "volume %s is creating", storage.LocalID)
		}
	} else {
		// fallback option to query all storages
		storages, _, err := d.xelon.PersistentStorages.List(ctx, d.tenantID)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		for _, storage := range storages {
			if storage.Name == volumeName {
				// storage was created if 'uuid' is not empty and 'formatted' is 1, otherwise create is in progress state
				if storage.UUID != "" && storage.Formatted == 1 {
					klog.V(2).InfoS("Volume already created",
						"method", "CreateVolume",
						"volume_id", storage.LocalID,
						"volume_name", volumeName,
					)
					return &csi.CreateVolumeResponse{
						Volume: &csi.Volume{
							VolumeId:      storage.LocalID,
							CapacityBytes: int64(storage.Capacity * giB),
						},
					}, nil
				} else {
					klog.V(2).InfoS("Volume is still creating",
						"method", "CreateVolume",
						"volume_id", storage.LocalID,
						"volume_name", volumeName,
					)
					return nil, status.Errorf(codes.AlreadyExists, "volume %s is creating", storage.LocalID)
				}
			}
		}
	}

	createRequest := &xelon.PersistentStorageCreateRequest{
		PersistentStorage: &xelon.PersistentStorage{
			Name: volumeName,
			Type: 2,
		},
		CloudID: d.cloudID,
		Size:    int(size / giB),
	}
	klog.V(5).InfoS("Creating persistent storage",
		"method", "CreateVolume",
		"payload", *createRequest,
		"tenant_id", d.tenantID,
	)
	apiResponse, _, err := d.xelon.PersistentStorages.Create(ctx, d.tenantID, createRequest)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	klog.V(5).InfoS("Created persistent storage",
		"method", "CreateVolume",
		"response", *apiResponse,
		"tenant_id", d.tenantID,
	)

	klog.V(2).InfoS("Waiting for the volume to get ready",
		"method", "CreateVolume",
		"volume_id", apiResponse.PersistentStorage.LocalID,
		"volume_name", volumeName,
	)
	if err = wait.PollUntilContextTimeout(ctx, volumeStatusCheckInterval, volumeStatusCheckTimeout, false, func(ctx context.Context) (bool, error) {
		storage, _, err := d.xelon.PersistentStorages.Get(ctx, d.tenantID, apiResponse.PersistentStorage.LocalID)
		if err != nil {
			return false, status.Error(codes.Internal, err.Error())
		}
		if storage.UUID != "" && storage.Formatted == 1 {
			return true, nil
		}
		return false, nil
	}); err != nil {
		return nil, status.Errorf(codes.Unknown, "volume is not ready")
	}

	klog.V(2).InfoS("Created volume successfully",
		"method", "CreateVolume",
		"volume_id", apiResponse.PersistentStorage.LocalID,
		"volume_name", volumeName,
	)
	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      apiResponse.PersistentStorage.LocalID,
			CapacityBytes: size,
		},
	}, nil
}

func (d *Driver) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "volume id not provided")
	}

	klog.V(2).InfoS("Deleting volume",
		"method", "DeleteVolume",
		"volume_id", req.VolumeId,
	)

	klog.V(5).InfoS("Delete persistent storage",
		"method", "DeleteVolume",
		"tenant_id", d.tenantID,
		"volume_id", req.VolumeId,
	)
	resp, err := d.xelon.PersistentStorages.Delete(ctx, d.tenantID, req.VolumeId)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			klog.V(2).InfoS("Volume was not found, assuming it was deleted externally",
				"method", "DeleteVolume",
				"response", *resp,
				"volume_id", req.VolumeId,
			)
			return &csi.DeleteVolumeResponse{}, nil
		}
		return nil, err
	}

	klog.V(2).InfoS("Deleted volume successfully",
		"method", "DeleteVolume",
		"volume_id", req.VolumeId,
	)
	return &csi.DeleteVolumeResponse{}, nil
}

func (d *Driver) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "volume id not provided")
	}
	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "node id not provided")
	}
	if req.VolumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "volume capability not provided")
	}

	klog.V(2).InfoS("Publishing volume",
		"method", "ControllerPublishVolume",
		"node_id", req.NodeId,
		"volume_id", req.VolumeId,
	)

	klog.V(5).InfoS("Fetching persistent storage to ensure it exists",
		"method", "ControllerPublishVolume",
		"tenant_id", d.tenantID,
		"volume_id", req.VolumeId,
	)
	storage, resp, err := d.xelon.PersistentStorages.Get(ctx, d.tenantID, req.VolumeId)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil, status.Errorf(codes.NotFound, "volume %q doesn't exist", req.VolumeId)
		}
		return nil, err
	}
	klog.V(5).InfoS("Found persistent storage",
		"method", "ControllerPublishVolume",
		"response", *storage,
		"tenant_id", d.tenantID,
		"volume_id", req.VolumeId,
	)

	klog.V(5).InfoS("Fetching device to ensure it exists",
		"method", "ControllerPublishVolume",
		"tenant_id", d.tenantID,
		"node_id", req.NodeId,
	)
	device, resp, err := d.xelon.Devices.Get(ctx, d.tenantID, req.NodeId)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil, status.Errorf(codes.NotFound, "device %q doesn't exist", req.NodeId)
		}
		return nil, err
	}
	if device == nil {
		return nil, status.Errorf(codes.Unknown, "device %q must not be nil", req.NodeId)
	}
	klog.V(5).InfoS("Found device",
		"method", "ControllerPublishVolume",
		"node_id", req.NodeId,
		"response", *device,
		"tenant_id", d.tenantID,
	)

	attachRequest := &xelon.PersistentStorageAttachDetachRequest{ServerID: []string{req.NodeId}}
	klog.V(5).InfoS("Attaching persistent storage to device",
		"method", "ControllerPublishVolume",
		"payload", *attachRequest,
		"tenant_id", d.tenantID,
		"volume_id", storage.LocalID,
	)
	apiResponse, _, err := d.xelon.PersistentStorages.AttachToDevice(ctx, d.tenantID, storage.LocalID, attachRequest)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	klog.V(5).InfoS("Attached persistent storage",
		"method", "ControllerPublishVolume",
		"response", *apiResponse,
		"tenant_id", d.tenantID,
		"volume_id", storage.LocalID,
	)

	klog.V(2).InfoS("Published volume",
		"method", "ControllerPublishVolume",
		"node_id", req.NodeId,
		"node_name", device.Device.LocalVMDetails.VMDisplayName,
		"volume_id", req.VolumeId,
	)
	return &csi.ControllerPublishVolumeResponse{
		PublishContext: map[string]string{
			xelonStorageUUID: storage.UUID,
			xelonStorageName: storage.Name,
		},
	}, nil
}

func (d *Driver) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "volume id not provided")
	}
	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "node id not provided")
	}

	klog.V(2).InfoS("Unpublishing volume",
		"method", "ControllerUnpublishVolume",
		"node_id", req.NodeId,
		"volume_id", req.VolumeId,
	)

	klog.V(5).InfoS("Fetching persistent storage to ensure it exists",
		"method", "ControllerUnpublishVolume",
		"tenant_id", d.tenantID,
		"volume_id", req.VolumeId,
	)
	storage, resp, err := d.xelon.PersistentStorages.Get(ctx, d.tenantID, req.VolumeId)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, err
	}
	klog.V(5).InfoS("Found persistent storage",
		"method", "ControllerUnpublishVolume",
		"response", *storage,
		"tenant_id", d.tenantID,
		"volume_id", req.VolumeId,
	)

	klog.V(5).InfoS("Fetching device to ensure it exists",
		"method", "ControllerUnpublishVolume",
		"tenant_id", d.tenantID,
		"node_id", req.NodeId,
	)
	device, resp, err := d.xelon.Devices.Get(ctx, d.tenantID, req.NodeId)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, err
	}
	if device == nil {
		return nil, status.Errorf(codes.Unknown, "device %q must not be nil", req.NodeId)
	}
	klog.V(5).InfoS("Found device",
		"method", "ControllerUnpublishVolume",
		"node_id", req.NodeId,
		"response", *device,
		"tenant_id", d.tenantID,
	)

	detachRequest := &xelon.PersistentStorageAttachDetachRequest{ServerID: []string{req.NodeId}}
	klog.V(5).InfoS("Detaching persistent storage from device",
		"method", "ControllerUnpublishVolume",
		"payload", *detachRequest,
		"tenant_id", d.tenantID,
		"volume_id", storage.LocalID,
	)
	apiResponse, resp, err := d.xelon.PersistentStorages.DetachFromDevice(ctx, d.tenantID, req.VolumeId, detachRequest)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, err
	}
	klog.V(5).InfoS("Detached persistent storage",
		"method", "ControllerUnpublishVolume",
		"response", *apiResponse,
		"tenant_id", d.tenantID,
		"volume_id", storage.LocalID,
	)

	klog.V(2).InfoS("Unpublished volume",
		"method", "ControllerUnpublishVolume",
		"node_id", req.NodeId,
		"node_name", device.Device.LocalVMDetails.VMDisplayName,
		"volume_id", req.VolumeId,
	)
	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (d *Driver) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID must be provided")
	}
	if req.VolumeCapabilities == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume Capabilities must be provided")
	}

	klog.V(2).InfoS("Validate volume capabilities",
		"method", "ValidateVolumeCapabilities",
		"volume_id", req.VolumeId,
		"volume_capabilities", req.VolumeCapabilities,
		"supported_capabilities", *supportedAccessMode,
	)

	klog.V(5).InfoS("Fetching persistent storage to ensure it exists",
		"method", "ValidateVolumeCapabilities",
		"tenant_id", d.tenantID,
		"volume_id", req.VolumeId,
	)
	storage, resp, err := d.xelon.PersistentStorages.Get(ctx, d.tenantID, req.VolumeId)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil, status.Errorf(codes.NotFound, "volume %q doesn't exist", req.VolumeId)
		}
	}
	klog.V(5).InfoS("Found persistent storage",
		"method", "ValidateVolumeCapabilities",
		"response", *storage,
		"tenant_id", d.tenantID,
		"volume_id", req.VolumeId,
	)

	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: []*csi.VolumeCapability{{
				AccessMode: supportedAccessMode,
			}},
		},
	}, nil
}

func (d *Driver) ListVolumes(_ context.Context, _ *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	klog.V(2).InfoS("Not yet implemented", "method", "ListVolumes")
	return nil, status.Error(codes.Unimplemented, "ListVolumes is not yet implemented")
}

func (d *Driver) GetCapacity(_ context.Context, _ *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	klog.V(2).InfoS("Not yet implemented", "method", "GetCapacity")
	return nil, status.Error(codes.Unimplemented, "GetCapacity is not yet implemented")
}

func (d *Driver) ControllerGetCapabilities(_ context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	logKV := []any{"method", "ControllerGetCapabilities", "req", *req}

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
	resp := &csi.ControllerGetCapabilitiesResponse{Capabilities: capabilities}
	logKV = append(logKV, "resp", *resp)
	klog.V(5).InfoS("Get supported capabilities of the controller server", logKV...)

	return resp, nil
}

func (d *Driver) CreateSnapshot(_ context.Context, _ *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	klog.V(2).InfoS("Not yet implemented", "method", "CreateSnapshot")
	return nil, status.Error(codes.Unimplemented, "CreateSnapshot is not yet implemented")
}

func (d *Driver) DeleteSnapshot(_ context.Context, _ *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	klog.V(2).InfoS("Not yet implemented", "method", "DeleteSnapshot")
	return nil, status.Error(codes.Unimplemented, "DeleteSnapshot is not yet implemented")
}

func (d *Driver) ListSnapshots(_ context.Context, _ *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	klog.V(2).InfoS("Not yet implemented", "method", "ListSnapshots")
	return nil, status.Error(codes.Unimplemented, "ListSnapshots is not yet implemented")
}

func (d *Driver) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "volume name not provided")
	}

	klog.V(2).InfoS("Expanding volume",
		"method", "ControllerExpandVolume",
		"volume_id", req.VolumeId,
	)

	klog.V(5).InfoS("Fetching persistent storage to ensure it exists",
		"method", "ControllerExpandVolume",
		"tenant_id", d.tenantID,
		"volume_id", req.VolumeId,
	)
	storage, resp, err := d.xelon.PersistentStorages.Get(ctx, d.tenantID, req.VolumeId)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil, status.Errorf(codes.NotFound, "volume %q doesn't exist", req.VolumeId)
		}
		return nil, status.Errorf(codes.Internal, "could not fetch existing volume: %v", err)
	}
	klog.V(5).InfoS("Found persistent storage",
		"method", "ControllerExpandVolume",
		"response", *storage,
		"tenant_id", d.tenantID,
		"volume_id", req.VolumeId,
	)

	resizeBytes, err := extractStorage(req.GetCapacityRange())
	if err != nil {
		return nil, status.Errorf(codes.OutOfRange, "invalid capacity range: %v", err)
	}

	if resizeBytes <= int64(storage.Capacity*giB) {
		klog.V(2).InfoS("Skip volume expanding because current volume size exceeds requested volume size",
			"current_volume_size_in_bytes", int64(storage.Capacity*giB),
			"method", "ControllerExpandVolume",
			"requested_volume_size_in_bytes", resizeBytes,
			"volume_id", req.VolumeId,
		)
		return &csi.ControllerExpandVolumeResponse{
			CapacityBytes:         int64(storage.Capacity * giB),
			NodeExpansionRequired: true,
		}, nil
	}

	extendRequest := &xelon.PersistentStorageExtendRequest{Size: int(resizeBytes / giB)}
	klog.V(5).InfoS("Extending persistent storage size",
		"method", "ControllerExpandVolume",
		"payload", *extendRequest,
		"tenant_id", d.tenantID,
		"volume_id", req.VolumeId,
	)
	apiResponse, _, err := d.xelon.PersistentStorages.Extend(ctx, req.VolumeId, extendRequest)
	if err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}
	klog.V(5).InfoS("Extended persistent storage",
		"method", "ControllerExpandVolume",
		"response", *apiResponse,
		"tenant_id", d.tenantID,
		"volume_id", req.VolumeId,
	)

	klog.V(2).InfoS("Waiting for the volume to get ready",
		"method", "ControllerExpandVolume",
		"volume_id", req.VolumeId,
	)
	if err = wait.PollUntilContextTimeout(ctx, volumeStatusCheckInterval, volumeStatusCheckTimeout, false, func(ctx context.Context) (bool, error) {
		storage, _, err := d.xelon.PersistentStorages.Get(ctx, d.tenantID, req.VolumeId)
		if err != nil {
			return false, status.Error(codes.Internal, err.Error())
		}
		if storage.UUID != "" && storage.Formatted == 1 {
			return true, nil
		}
		return false, nil
	}); err != nil {
		return nil, status.Errorf(codes.Unknown, "volume is not ready")
	}

	klog.V(2).InfoS("Resized volume successfully",
		"method", "ControllerExpandVolume",
		"new_volume_size", resizeBytes,
		"volume_id", req.VolumeId,
	)

	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         resizeBytes,
		NodeExpansionRequired: true,
	}, nil
}

func (d *Driver) ControllerGetVolume(_ context.Context, _ *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	klog.V(2).InfoS("Not yet implemented", "method", "ControllerGetVolume")
	return nil, status.Error(codes.Unimplemented, "ControllerGetVolume is not yet implemented")
}

func (d *Driver) ControllerModifyVolume(_ context.Context, _ *csi.ControllerModifyVolumeRequest) (*csi.ControllerModifyVolumeResponse, error) {
	klog.V(2).InfoS("Not yet implemented", "method", "ControllerModifyVolume")
	return nil, status.Error(codes.Unimplemented, "ControllerModifyVolume is not yet implemented")
}

// extractStorage extracts the storage size in bytes from the given capacity range. If the capacity
// range is not satisfied it returns the default volume size. If the capacity range is below or
// above supported sizes, it returns an error.
func extractStorage(capacityRange *csi.CapacityRange) (int64, error) {
	if capacityRange == nil {
		return defaultVolumeSizeInBytes, nil
	}

	requiredBytes := capacityRange.GetRequiredBytes()
	requiredBytesSet := 0 < requiredBytes
	limitBytes := capacityRange.GetLimitBytes()
	limitBytesSet := 0 < limitBytes

	if !requiredBytesSet && !limitBytesSet {
		return defaultVolumeSizeInBytes, nil
	}

	if requiredBytesSet && limitBytesSet && (limitBytes < requiredBytes) {
		return 0, fmt.Errorf("limit (%v) cannot be less then required (%v) size", formatBytes(limitBytes), formatBytes(requiredBytes))
	}

	if requiredBytesSet && !limitBytesSet && requiredBytes < minVolumeSizeInBytes {
		return 0, fmt.Errorf("required (%v) can not be less than minimum supported volume size (%v)", formatBytes(requiredBytes), formatBytes(minVolumeSizeInBytes))
	}

	if requiredBytesSet {
		return requiredBytes, nil
	}
	if limitBytesSet {
		return limitBytes, nil
	}

	return minVolumeSizeInBytes, nil
}

func formatBytes(inputBytes int64) string {
	output := float64(inputBytes)
	unit := ""

	switch {
	case inputBytes >= tiB:
		output = output / tiB
		unit = "Ti"
	case inputBytes >= giB:
		output = output / giB
		unit = "Gi"
	case inputBytes >= miB:
		output = output / miB
		unit = "Mi"
	case inputBytes >= kiB:
		output = output / kiB
		unit = "Ki"
	case inputBytes == 0:
		return "0"
	}

	result := strconv.FormatFloat(output, 'f', 1, 64)
	result = strings.TrimSuffix(result, ".0")
	return result + unit
}
