package driver

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Xelon-AG/xelon-sdk-go/xelon"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog"
)

const (
	_   = iota
	kiB = 1 << (10 * iota)
	miB
	giB
	tiB
)

const (
	minVolumeSizeInBytes     int64 = 5 * giB
	defaultVolumeSizeInBytes int64 = 10 * giB

	volumeStatusCheckRetries  = 20
	volumeStatusCheckInterval = 6
)

var (
	// controllerCapabilities represents the capabilities of the Xelon Volumes
	controllerCapabilities = []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
	}

	xelonStorageLocalID = DefaultDriverName + "/storage-id"
	xelonStorageName    = DefaultDriverName + "/storage-name"
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
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "Name must be provided")
	}
	if req.VolumeCapabilities == nil || len(req.VolumeCapabilities) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume capabilities must be provided")
	}

	// TODO: validation

	size, err := extractStorage(req.CapacityRange)
	if err != nil {
		return nil, status.Errorf(codes.OutOfRange, "Invalid capacity range: %v", err)
	}

	volumeName := req.Name

	log := d.log.WithFields(logrus.Fields{
		"method":                 "create_volume",
		"storage_size_gigabytes": size / giB,
		"volume_capabilities":    req.VolumeCapabilities,
		"volume_name":            volumeName,
	})
	log.Info("create volume called")

	storages, _, err := d.xelon.PersistentStorages.List(ctx, d.tenantID)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	for _, storage := range storages {
		if storage.Name == volumeName {

			log.Info("volume already created")
			return &csi.CreateVolumeResponse{
				Volume: &csi.Volume{
					VolumeId:      storage.LocalID,
					CapacityBytes: int64(storage.Capacity * giB),
				},
			}, nil
		}
	}

	createRequest := &xelon.PersistentStorageCreateRequest{
		PersistentStorage: &xelon.PersistentStorage{
			Name: volumeName,
			Type: 2,
		},
		Size: int(size / giB),
	}
	log.WithField("volume_create_request", createRequest).Info("creating volume")
	apiResponse, _, err := d.xelon.PersistentStorages.Create(ctx, d.tenantID, createRequest)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// check to see if volume is in active state
	volumeReady := false
	for i := 0; i < volumeStatusCheckRetries; i++ {
		time.Sleep(volumeStatusCheckInterval * time.Second)
		storage, _, err := d.xelon.PersistentStorages.Get(ctx, d.tenantID, apiResponse.PersistentStorage.LocalID)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		if storage.BlockStorage.Status == 1 {
			volumeReady = true
			break
		}
	}
	if !volumeReady {
		return nil, status.Errorf(codes.Internal, "volume is not ready %v seconds", volumeStatusCheckRetries*volumeStatusCheckInterval)
	}

	resp := &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      apiResponse.PersistentStorage.LocalID,
			CapacityBytes: size,
		},
	}

	log.WithField("response", resp).Info("volume was created")
	return resp, nil
}

func (d *Driver) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID must be provided")
	}

	log := d.log.WithFields(logrus.Fields{
		"method":    "delete_volume",
		"volume_id": req.VolumeId,
	})
	log.Info("delete volume called")

	resp, err := d.xelon.PersistentStorages.Delete(ctx, d.tenantID, req.VolumeId)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			log.WithFields(logrus.Fields{
				"error":    err,
				"response": resp,
			}).Warn("assuming volume is deleted because it does not exist")
			return &csi.DeleteVolumeResponse{}, nil
		}
		return nil, err
	}

	log.WithField("response", resp).Info("volume was deleted")
	return &csi.DeleteVolumeResponse{}, nil
}

// ControllerPublishVolume attaches the given volume to the node
func (d *Driver) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID must be provided")
	}
	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Node ID must be provided")
	}
	if req.VolumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capability must be provided")
	}

	log := d.log.WithFields(logrus.Fields{
		"method":    "controller_publish_volume",
		"node_id":   req.NodeId,
		"volume_id": req.VolumeId,
	})
	log.Info("controller publish volume called")

	// check if storage exist before attaching it
	storage, resp, err := d.xelon.PersistentStorages.Get(ctx, d.tenantID, req.VolumeId)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil, status.Errorf(codes.NotFound, "volume %q doesn't exist", req.VolumeId)
		}
		return nil, err
	}

	// check if device exist before attaching to it
	_, resp, err = d.xelon.Devices.Get(ctx, d.tenantID, req.NodeId)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil, status.Errorf(codes.NotFound, "device %q doesn't exist", req.NodeId)
		}
		return nil, err
	}

	attachRequest := &xelon.PersistentStorageAttachDetachRequest{
		ServerID: []string{req.NodeId},
	}
	_, _, err = d.xelon.PersistentStorages.AttachToDevice(ctx, d.tenantID, storage.LocalID, attachRequest)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	log.Info("volume was attached")
	return &csi.ControllerPublishVolumeResponse{
		PublishContext: map[string]string{
			xelonStorageLocalID: storage.LocalID,
			xelonStorageName:    storage.Name,
		},
	}, nil
}

// ControllerUnpublishVolume detaches the given volume from the node
func (d *Driver) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID must be provided")
	}
	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Node ID must be provided")
	}

	log := d.log.WithFields(logrus.Fields{
		"method":    "controller_unpublish_volume",
		"node_id":   req.NodeId,
		"volume_id": req.VolumeId,
	})
	log.Info("controller unpublish volume called")

	// check if storage exist before detaching it
	_, resp, err := d.xelon.PersistentStorages.Get(ctx, d.tenantID, req.VolumeId)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			log.Info("assuming storage is detached because it does not exist")
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, err
	}

	// check if device exist before attaching to it
	_, resp, err = d.xelon.Devices.Get(ctx, d.tenantID, req.NodeId)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			log.Info("storage cannot be detached from deleted devices")
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, err
	}

	detachRequest := &xelon.PersistentStorageAttachDetachRequest{
		ServerID: []string{req.NodeId},
	}
	_, resp, err = d.xelon.PersistentStorages.DetachFromDevice(ctx, d.tenantID, req.VolumeId, detachRequest)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			log.WithFields(logrus.Fields{
				"error": err,
				"resp":  resp,
			}).Warn("storage is not attached to device")
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, err
	}

	log.Info("volume was detached")
	return &csi.ControllerUnpublishVolumeResponse{}, nil
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
