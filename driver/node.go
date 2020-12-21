package driver

import (
	"context"
	"os"
	"path"
	"path/filepath"

	"github.com/Xelon-AG/xelon-csi/driver/helper"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog"
)

const diskUUIDPath = "/dev/disk/by-uuid"

type nodeService struct {
	mounter helper.Mounter

	nodeID string
}

func (d *Driver) newNodeService(config *Config) error {
	d.log.Info("Initializing Xelon node service...")

	localVMID, err := helper.GetDeviceLocalVMID(config.MetadataFile)
	if err != nil {
		return err
	}

	d.log.Infof("Node ID: %s", localVMID)

	d.nodeService = &nodeService{
		mounter: helper.NewMounter(d.log),
		nodeID:  localVMID,
	}

	return nil
}

// NodeStageVolume mounts the volume to a staging path on the node. This is
// called by the CO before NodePublishVolume and is used to temporary mount the
// volume to a staging path. Once mounted, NodePublishVolume will make sure to
// mount it to the appropriate path
func (d *Driver) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Errorf(codes.InvalidArgument, "Volume ID must be provided")
	}
	if req.StagingTargetPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "Staging target path must be provided")
	}
	if req.VolumeCapability == nil {
		return nil, status.Errorf(codes.InvalidArgument, "Volume capability must be provided")
	}

	log := d.log.WithFields(logrus.Fields{
		"method":              "node_stage_volume",
		"staging_target_path": req.StagingTargetPath,
		"volume_id":           req.VolumeId,
	})
	log.Info("node stage volume called")

	volumeName, ok := req.GetPublishContext()[xelonStorageName]
	if !ok || volumeName == "" {
		return nil, status.Errorf(codes.InvalidArgument, "%s not found in publish context of volume %s", xelonStorageName, req.VolumeId)
	}
	volumeUUID, ok := req.GetPublishContext()[xelonStorageUUID]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "%s not found in publish context of volume %s", volumeUUID, req.VolumeId)
	}

	source, err := getDevicePathByUUID(volumeUUID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, status.Errorf(codes.NotFound, "volume %s is not mounted on node yet", req.VolumeId)
		}
		return nil, status.Errorf(codes.Internal, "error getting device path for volume with ID %s: %s", req.VolumeId, err.Error())
	}
	target := req.StagingTargetPath
	mnt := req.VolumeCapability.GetMount()
	options := mnt.MountFlags

	log = d.log.WithFields(logrus.Fields{
		"mount_options":   options,
		"publish_context": req.PublishContext,
		"source":          source,
		"volume_context":  req.VolumeContext,
		"volume_name":     volumeName,
	})

	formatted, err := d.mounter.IsFormatted(source)
	if err != nil {
		return nil, err
	}
	if !formatted {
		log.Info("the volume is not formatted for staging")
		return nil, status.Errorf(codes.Internal, "the volume %s is not formatted", source)
	}

	log.Info("mount the volume for staging")

	mounted, err := d.mounter.IsMounted(target)
	if err != nil {
		return nil, err
	}

	if !mounted {
		err := d.mounter.Mount(source, target, options...)
		if err != nil {
			return nil, status.Errorf(codes.Internal, err.Error())
		}
	} else {
		log.Info("source device is already mounted to the target path")
	}

	log.Info("mounting stage volume is finished")
	return &csi.NodeStageVolumeResponse{}, nil
}

// NodeUnstageVolume unstages the volume from the staging path
func (d *Driver) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID must be provided")
	}

	if req.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "Staging Target Path must be provided")
	}

	log := d.log.WithFields(logrus.Fields{
		"method":              "node_unstage_volume",
		"staging_target_path": req.StagingTargetPath,
		"volume_id":           req.VolumeId,
	})
	log.Info("node unstage volume called")

	mounted, err := d.mounter.IsMounted(req.StagingTargetPath)
	if err != nil {
		return nil, err
	}

	if mounted {
		log.Info("unmounting the staging target path")
		err := d.mounter.Unmount(req.StagingTargetPath)
		if err != nil {
			return nil, err
		}
	} else {
		log.Info("staging target path is already unmounted")
	}

	log.Info("unmounting stage volume is finished")
	return &csi.NodeUnstageVolumeResponse{}, nil
}

// NodePublishVolume mounts the volume mounted to the staging path to the target path
func (d *Driver) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Volume ID must be provided")
	}
	if req.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Staging Target Path must be provided")
	}
	if req.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Target Path must be provided")
	}
	if req.VolumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Volume Capability must be provided")
	}

	log := d.log.WithFields(logrus.Fields{
		"method":              "node_publish_volume",
		"staging_target_path": req.StagingTargetPath,
		"target_path":         req.TargetPath,
		"volume_id":           req.VolumeId,
	})
	log.Info("node publish volume called")

	klog.V(4).Infof("NodePublishVolume called")

	source := req.StagingTargetPath
	target := req.TargetPath
	mountOptions := []string{"bind"}

	mnt := req.VolumeCapability.GetMount()
	mountOptions = append(mountOptions, mnt.MountFlags...)

	mounted, err := d.mounter.IsMounted(target)
	if err != nil {
		return nil, err
	}

	log = log.WithFields(logrus.Fields{
		"source_path":   source,
		"mount_options": mountOptions,
	})

	if !mounted {
		log.Info("mounting the volume")
		if err := d.mounter.Mount(source, target, mountOptions...); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	} else {
		log.Info("volume is already mounted")
	}

	log.Info("bind mounting the volume is finished")
	return &csi.NodePublishVolumeResponse{}, nil
}

// NodeUnpublishVolume unmounts the volume from the target path
func (d *Driver) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID must be provided")
	}

	if req.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "Target Path must be provided")
	}

	log := d.log.WithFields(logrus.Fields{
		"method":      "node_unpublish_volume",
		"target_path": req.TargetPath,
		"volume_id":   req.VolumeId,
	})
	log.Info("node unpublish volume called")

	mounted, err := d.mounter.IsMounted(req.TargetPath)
	if err != nil {
		return nil, err
	}

	if mounted {
		log.Info("unmounting the target path")
		err := d.mounter.Unmount(req.TargetPath)
		if err != nil {
			return nil, err
		}
	} else {
		log.Info("target path is already unmounted")
	}

	log.Info("unmounting volume is finished")
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// NodeGetCapabilities returns the supported capabilities of the node server
func (d *Driver) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
					},
				},
			},
		},
	}, nil
}

// NodeGetInfo returns the supported capabilities of the node server. The result of this
// function will be used by the CO in ControllerPublishVolume.
func (d *Driver) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	d.log.WithField("method", "node_get_info").Info("node get info called")
	return &csi.NodeGetInfoResponse{
		NodeId:            d.nodeService.nodeID,
		MaxVolumesPerNode: 15,
	}, nil
}

// NodeGetVolumeStats returns the volume capacity statistics available for the
// the given volume.
func (d *Driver) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	klog.V(4).Infof("NodeGetVolumeStats is not yet implemented")
	return nil, status.Error(codes.Unimplemented, "NodeGetVolumeStats is not yet implemented")
}

// NodeExpandVolume expands the given volume
func (d *Driver) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	klog.V(4).Infof("NodeExpandVolume is not yet implemented")
	return nil, status.Error(codes.Unimplemented, "NodeExpandVolume is not yet implemented")
}

func getDevicePathByUUID(volumeUUID string) (string, error) {
	devicePath := path.Join(diskUUIDPath, volumeUUID)
	realDevicePath, err := filepath.EvalSymlinks(devicePath)
	if err != nil {
		return "", err
	}

	deviceInfo, err := os.Stat(realDevicePath)
	if err != nil {
		return "", err
	}

	deviceMode := deviceInfo.Mode()
	if os.ModeDevice != deviceMode&os.ModeDevice || os.ModeCharDevice == deviceMode&os.ModeCharDevice {
		return "", errDevicePathIsNotDevice
	}

	return realDevicePath, nil
}
