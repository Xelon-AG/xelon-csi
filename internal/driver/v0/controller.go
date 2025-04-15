package v0

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"

	driverv1 "github.com/Xelon-AG/xelon-csi/internal/driver"
	"github.com/Xelon-AG/xelon-csi/internal/driver/v0/helper"
	"github.com/Xelon-AG/xelon-sdk-go/xelon"
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

	volumeStatusCheckRetries  = 30
	volumeStatusCheckInterval = 10
)

var (
	// controllerCapabilities represents the capabilities of the Xelon Volumes
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

	xelonStorageUUID = driverv1.DefaultDriverName + "/storage-uuid"
	xelonStorageName = driverv1.DefaultDriverName + "/storage-name"
)

type controllerService struct {
	xelon    *xelon.Client
	cloudID  string
	tenantID string
}

func (d *DriverV0) initializeControllerService(config *Config) error {
	d.log.Info("Initializing Xelon controller service")

	userAgent := fmt.Sprintf("%s/%s (%s)", driverv1.DefaultDriverName, driverVersion, gitCommit)

	opts := []xelon.ClientOption{xelon.WithUserAgent(userAgent)}
	opts = append(opts, xelon.WithBaseURL(d.config.BaseURL))
	if d.config.ClientID != "" {
		opts = append(opts, xelon.WithClientID(d.config.ClientID))
	}
	client := xelon.NewClient(d.config.Token, opts...)

	tenant, _, err := client.Tenants.GetCurrent(context.Background())
	if err != nil {
		return err
	}

	d.log.Infof("Tenant ID: %s", tenant.TenantID)

	cloudID, err := helper.GetDeviceCloudID(config.MetadataFile)
	if err != nil {
		return err
	}

	d.controllerService = &controllerService{
		xelon:    client,
		cloudID:  cloudID,
		tenantID: tenant.TenantID,
	}

	return nil
}

// CreateVolume creates a new volume with the given CreateVolumeRequest.
func (d *DriverV0) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "Name must be provided")
	}
	if len(req.VolumeCapabilities) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume capabilities must be provided")
	}
	if !isValidCapabilities(req.VolumeCapabilities) {
		return nil, status.Errorf(codes.InvalidArgument, "Volume capability is not compatible: %v", req)
	}

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

	storage, response, err := d.xelon.PersistentStorages.GetByName(ctx, d.tenantID, volumeName)
	if err != nil && response != nil && response.StatusCode != http.StatusNotFound {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if storage != nil {
		if storage.UUID != "" && storage.Formatted == 1 {
			log.Info("volume already created")
			return &csi.CreateVolumeResponse{
				Volume: &csi.Volume{
					VolumeId:      storage.LocalID,
					CapacityBytes: int64(storage.Capacity * giB),
				},
			}, nil
		} else {
			log.WithField("volume_id", storage.LocalID).Info("volume is creating")
			return nil, status.Errorf(codes.Aborted, "Volume %s is creating", storage.LocalID)
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
					log.Info("volume already created")
					return &csi.CreateVolumeResponse{
						Volume: &csi.Volume{
							VolumeId:      storage.LocalID,
							CapacityBytes: int64(storage.Capacity * giB),
						},
					}, nil
				} else {
					log.WithField("volume_id", storage.LocalID).Info("volume is creating")
					return nil, status.Errorf(codes.Aborted, "Volume %s is creating", storage.LocalID)
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
	log.WithField("volume_create_request", createRequest).Info("creating volume")
	apiResponse, _, err := d.xelon.PersistentStorages.Create(ctx, d.tenantID, createRequest)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	volumeReady := false
	for i := 0; i < volumeStatusCheckRetries; i++ {
		time.Sleep(volumeStatusCheckInterval * time.Second)
		storage, _, err := d.xelon.PersistentStorages.Get(ctx, d.tenantID, apiResponse.PersistentStorage.LocalID)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		if storage.UUID != "" && storage.Formatted == 1 {
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

func (d *DriverV0) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
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

	log.Info("volume was deleted")
	return &csi.DeleteVolumeResponse{}, nil
}

// ControllerPublishVolume attaches the given volume to the node
func (d *DriverV0) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
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
	nodeName := "unknown"
	device, resp, err := d.xelon.Devices.Get(ctx, d.tenantID, req.NodeId)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil, status.Errorf(codes.NotFound, "device %q doesn't exist", req.NodeId)
		}
		return nil, err
	}
	if device != nil {
		nodeName = device.Device.LocalVMDetails.VMDisplayName
	} else {
		log.Warn("node name could not be obtained because device is nil")
	}

	attachRequest := &xelon.PersistentStorageAttachDetachRequest{
		ServerID: []string{req.NodeId},
	}
	_, _, err = d.xelon.PersistentStorages.AttachToDevice(ctx, d.tenantID, storage.LocalID, attachRequest)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	log.WithFields(logrus.Fields{
		"node_name": nodeName,
	}).Info("volume was attached")

	return &csi.ControllerPublishVolumeResponse{
		PublishContext: map[string]string{
			xelonStorageUUID: storage.UUID,
			xelonStorageName: storage.Name,
		},
	}, nil
}

// ControllerUnpublishVolume detaches the given volume from the node
func (d *DriverV0) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
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
	nodeName := "unknown"
	device, resp, err := d.xelon.Devices.Get(ctx, d.tenantID, req.NodeId)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			log.Info("storage cannot be detached from deleted devices")
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, err
	}
	if device != nil {
		nodeName = device.Device.LocalVMDetails.VMDisplayName
	} else {
		log.Warn("node name could not be obtained because device is nil")
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

	log.WithFields(logrus.Fields{
		"node_name": nodeName,
	}).Info("volume was detached")
	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (d *DriverV0) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "ValidateVolumeCapabilities Volume ID must be provided")
	}
	if req.VolumeCapabilities == nil {
		return nil, status.Error(codes.InvalidArgument, "ValidateVolumeCapabilities Volume Capabilities must be provided")
	}

	log := d.log.WithFields(logrus.Fields{
		"method":                 "validate_volume_capabilities",
		"volume_id":              req.VolumeId,
		"volume_capabilities":    req.VolumeCapabilities,
		"supported_capabilities": supportedAccessMode,
	})
	log.Info("validate volume capabilities called")

	// check if storage exist before validating it
	_, resp, err := d.xelon.PersistentStorages.Get(ctx, d.tenantID, req.VolumeId)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil, status.Errorf(codes.NotFound, "volume %q doesn't exist", req.VolumeId)
		}
	}

	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessMode: supportedAccessMode,
				},
			},
		},
	}, nil
}

func (d *DriverV0) ListVolumes(_ context.Context, _ *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	klog.V(4).Infof("ListVolumes is not yet implemented")
	return nil, status.Error(codes.Unimplemented, "")
}

func (d *DriverV0) GetCapacity(_ context.Context, _ *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	klog.V(4).Infof("GetCapacity is not yet implemented")
	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerGetCapabilities get capabilities of the Xelon controller.
func (d *DriverV0) ControllerGetCapabilities(_ context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
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
func (d *DriverV0) CreateSnapshot(_ context.Context, _ *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	klog.V(4).Infof("CreateSnapshot is not yet implemented")
	return nil, status.Error(codes.Unimplemented, "CreateSnapshot is not yet implemented")
}

// DeleteSnapshot will be called by the CO to delete a snapshot.
func (d *DriverV0) DeleteSnapshot(_ context.Context, _ *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	klog.V(4).Infof("DeleteSnapshot is not yet implemented")
	return nil, status.Error(codes.Unimplemented, "DeleteSnapshot is not yet implemented")
}

// ListSnapshots returns the information about all snapshots on the storage
// system within the given parameters regardless of how they were created.
func (d *DriverV0) ListSnapshots(_ context.Context, _ *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	klog.V(4).Infof("ListSnapshots is not yet implemented")
	return nil, status.Error(codes.Unimplemented, "ListSnapshots is not yet implemented")
}

// ControllerExpandVolume is called from the resizer to increase the volume size.
func (d *DriverV0) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	volumeID := req.GetVolumeId()

	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "ControllerExpandVolume volume ID missing in request")
	}

	storage, _, err := d.xelon.PersistentStorages.Get(ctx, d.tenantID, req.VolumeId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "ControllerExpandVolume could not retrieve existing volume: %v", err)
	}

	resizeBytes, err := extractStorage(req.GetCapacityRange())
	if err != nil {
		return nil, status.Errorf(codes.OutOfRange, "ControllerExpandVolume invalid capacity range: %v", err)
	}

	log := d.log.WithFields(logrus.Fields{
		"method":                 "expand_volume",
		"storage_size_gigabytes": resizeBytes / giB,
		"volume_id":              volumeID,
	})
	log.Info("expand volume called")

	if resizeBytes <= int64(storage.Capacity*giB) {
		log.WithFields(logrus.Fields{
			"current_volume_size":   int64(storage.Capacity * giB),
			"requested_volume_size": resizeBytes,
		}).Info("skipping volume resize because current volume size exceeds requested volume size")

		return &csi.ControllerExpandVolumeResponse{
			CapacityBytes:         int64(storage.Capacity * giB),
			NodeExpansionRequired: true,
		}, nil
	}

	extendRequest := &xelon.PersistentStorageExtendRequest{
		Size: int(resizeBytes / giB),
	}
	log.WithField("volume_extend_request", extendRequest).Info("extending volume")
	apiResponse, _, err := d.xelon.PersistentStorages.Extend(ctx, volumeID, extendRequest)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not extend volume: %v: %v", apiResponse, err)
	}

	volumeReady := false
	for i := 0; i < volumeStatusCheckRetries; i++ {
		time.Sleep(volumeStatusCheckInterval * time.Second)
		storage, _, err := d.xelon.PersistentStorages.Get(ctx, d.tenantID, volumeID)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		if storage.UUID != "" && storage.Formatted == 1 {
			volumeReady = true
			break
		}
	}
	if !volumeReady {
		return nil, status.Errorf(codes.Internal, "volume is not ready %v seconds", volumeStatusCheckRetries*volumeStatusCheckInterval)
	}

	log.WithField("new_volume_size", resizeBytes).Info("volume was resized")

	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         resizeBytes,
		NodeExpansionRequired: true,
	}, nil
}

// ControllerGetVolume gets a specific volume.
func (d *DriverV0) ControllerGetVolume(_ context.Context, _ *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	klog.V(4).Infof("ControllerGetVolume is not yet implemented")
	return nil, status.Error(codes.Unimplemented, "ControllerGetVolume is not yet implemented")
}

func (d *DriverV0) ControllerModifyVolume(_ context.Context, _ *csi.ControllerModifyVolumeRequest) (*csi.ControllerModifyVolumeResponse, error) {
	klog.V(4).Infof("ControllerModifyVolume is not yet implemented")
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

// isValidCapabilities validates the requested capabilities. It returns a list
// of violations which may be empty if no violations were found.
func isValidCapabilities(capabilities []*csi.VolumeCapability) bool {
	for _, capability := range capabilities {
		if capability == nil {
			return false
		}

		accessMode := capability.GetAccessMode()
		if accessMode == nil {
			return false
		}

		if accessMode.GetMode() != supportedAccessMode.GetMode() {
			return false
		}
	}
	return true
}
