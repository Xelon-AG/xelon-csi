package driver

import (
	"context"
	"os"
	"path"
	"path/filepath"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog"
	"k8s.io/mount-utils"
	"k8s.io/utils/exec"

	"github.com/Xelon-AG/xelon-csi/driver/helper"
)

const diskUUIDPath = "/dev/disk/by-uuid"

type nodeService struct {
	mounter helper.Mounter

	nodeID   string
	nodeName string
}

func (d *Driver) newNodeService(config *Config) error {
	d.log.Info("Initializing Xelon node service...")

	localVMID, err := helper.GetDeviceLocalVMID(config.MetadataFile)
	if err != nil {
		return err
	}
	hostname, err := helper.GetDeviceHostname(config.MetadataFile)
	if err != nil {
		return err
	}

	d.log.Infof("Node Name: %s, ID: %s", hostname, localVMID)

	d.nodeService = &nodeService{
		mounter:  helper.NewMounter(d.log),
		nodeID:   localVMID,
		nodeName: hostname,
	}

	return nil
}

// NodeStageVolume mounts the volume to a staging path on the node. This is
// called by the CO before NodePublishVolume and is used to temporary mount the
// volume to a staging path. Once mounted, NodePublishVolume will make sure to
// mount it to the appropriate path
func (d *Driver) NodeStageVolume(_ context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
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
		"node_name":           d.nodeName,
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

	if d.config.RescanOnResize {
		if err := d.mounter.RescanSCSIDevices(); err != nil {
			return nil, status.Errorf(codes.Internal, "NodeStageVolume error rescanning scsi devices %q: %v", req.VolumeId, err)
		}
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
func (d *Driver) NodeUnstageVolume(_ context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID must be provided")
	}

	if req.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "Staging Target Path must be provided")
	}

	log := d.log.WithFields(logrus.Fields{
		"method":              "node_unstage_volume",
		"node_name":           d.nodeName,
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

	if d.config.RescanOnResize {
		if err := d.mounter.RescanSCSIDevices(); err != nil {
			return nil, status.Errorf(codes.Internal, "NodeUnstageVolume error rescanning scsi devices %q: %v", req.VolumeId, err)
		}
	}

	log.Info("unmounting stage volume is finished")
	return &csi.NodeUnstageVolumeResponse{}, nil
}

// NodePublishVolume mounts the volume mounted to the staging path to the target path
func (d *Driver) NodePublishVolume(_ context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
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
func (d *Driver) NodeUnpublishVolume(_ context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
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
func (d *Driver) NodeGetCapabilities(_ context.Context, _ *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
					},
				},
			},
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
					},
				},
			},
		},
	}, nil
}

// NodeGetInfo returns the supported capabilities of the node server. The result of this
// function will be used by the CO in ControllerPublishVolume.
func (d *Driver) NodeGetInfo(_ context.Context, _ *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	d.log.WithFields(logrus.Fields{
		"node_name": d.nodeName,
		"method":    "node_get_info",
	}).Info("node get info called")
	return &csi.NodeGetInfoResponse{
		NodeId:            d.nodeService.nodeID,
		MaxVolumesPerNode: 15,
	}, nil
}

// NodeGetVolumeStats returns the volume capacity statistics available for the
// the given volume.
func (d *Driver) NodeGetVolumeStats(_ context.Context, _ *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	klog.V(4).Infof("NodeGetVolumeStats is not yet implemented")
	return nil, status.Error(codes.Unimplemented, "NodeGetVolumeStats is not yet implemented")
}

// NodeExpandVolume expands the given volume
func (d *Driver) NodeExpandVolume(_ context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeExpandVolume volume ID not provided")
	}
	volumePath := req.GetVolumePath()
	if volumePath == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeExpandVolume volume path not provided")
	}

	log := d.log.WithFields(logrus.Fields{
		"volume_id":   volumeID,
		"volume_path": volumePath,
		"method":      "node_expand_volume",
	})
	log.Info("node expand volume called")

	mounted, err := d.mounter.IsMounted(volumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "NodeExpandVolume failed to check if volume path %q is mounted: %s", volumePath, err)
	}
	if !mounted {
		return nil, status.Errorf(codes.NotFound, "NodeExpandVolume volume path %q is not mounted", volumePath)
	}

	mounter := mount.New("")
	devicePath, _, err := mount.GetDeviceNameFromMount(mounter, volumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "NodeExpandVolume unable to get device path for %q: %v", volumePath, err)
	}

	if d.config.RescanOnResize {
		if err = d.mounter.RescanSCSIDevices(); err != nil {
			return nil, status.Errorf(codes.Internal, "NodeExpandVolume could not rescan devices %q: %v", volumeID, err)
		}
	}

	r := mount.NewResizeFs(exec.New())
	log = log.WithFields(logrus.Fields{
		"device_path": devicePath,
	})
	log.Info("resizing volume")
	if _, err := r.Resize(devicePath, volumePath); err != nil {
		return nil, status.Errorf(codes.Internal, "NodeExpandVolume could not resize volume %q (%q):  %v", volumeID, req.GetVolumePath(), err)
	}

	log.Info("volume was resized")
	return &csi.NodeExpandVolumeResponse{}, nil
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
