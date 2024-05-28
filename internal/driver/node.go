package driver

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
	"k8s.io/mount-utils"
	"k8s.io/utils/exec"

	"github.com/Xelon-AG/xelon-csi/internal/driver/cloud"
)

const (
	diskUUIDPath = "/dev/disk/by-uuid"

	maxVolumeCountPerNode = 15
)

var (
	nodeCapabilities = []csi.NodeServiceCapability_RPC_Type{
		csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
		csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
		// csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
	}
)

type nodeService struct {
	mounter *mount.SafeFormatAndMount

	nodeID         string
	nodeName       string
	rescanOnResize bool
}

func newNodeService(ctx context.Context, opts *Options) (*nodeService, error) {
	klog.V(2).InfoS("Initialize node service")

	metadata, err := cloud.RetrieveMetadata(ctx)
	if err != nil {
		return nil, err
	}
	klog.V(5).InfoS("Retrieved device metadata", "metadata", *metadata)

	if metadata.LocalVMID == "" {
		return nil, errors.New("localVMID cannot be empty")
	}

	return &nodeService{
		mounter: &mount.SafeFormatAndMount{
			Interface: mount.New(""),
			Exec:      exec.New(),
		},
		nodeID:         metadata.LocalVMID,
		nodeName:       metadata.Name,
		rescanOnResize: opts.RescanOnResize,
	}, nil
}

func (d *Driver) NodeStageVolume(_ context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Errorf(codes.InvalidArgument, "volume id not provided")
	}
	if req.StagingTargetPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "staging target path not provided")
	}
	if req.VolumeCapability == nil {
		return nil, status.Errorf(codes.InvalidArgument, "volume capability not provided")
	}

	klog.V(2).InfoS("Mounting volume to staging path",
		"method", "NodeStageVolume",
		"node_name", d.nodeName,
		"staging_target_path", req.StagingTargetPath,
		"volume_id", req.VolumeId,
	)

	volumeName, ok := req.GetPublishContext()[xelonStorageName]
	if !ok || volumeName == "" {
		return nil, status.Errorf(codes.InvalidArgument, "%s not found in publish context of volume %s", xelonStorageName, req.VolumeId)
	}
	volumeUUID, ok := req.GetPublishContext()[xelonStorageUUID]
	if !ok || volumeUUID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "%s not found in publish context of volume %s", xelonStorageUUID, req.VolumeId)
	}

	if d.rescanOnResize {
		if err := cloud.RescanSCSIDevices(); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to rescan volume: %s", err)
		}
	}

	devicePath, err := getDevicePathByUUID(volumeUUID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, status.Errorf(codes.NotFound, "volume %s is not mounted on node yet", req.VolumeId)
		}
		return nil, status.Errorf(codes.Internal, "error getting device path for volume with ID %s: %s", req.VolumeId, err.Error())
	}
	target := req.StagingTargetPath

	klog.V(5).InfoS("Determining if staging target is not a mount point",
		"method", "NodeStageVolume",
		"node_id", d.nodeID,
		"node_name", d.nodeName,
		"staging_target_path", target,
		"volume_id", req.VolumeId,
	)
	notMnt, err := d.mounter.IsLikelyNotMountPoint(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			err = os.MkdirAll(target, 0750)
			if err != nil {
				klog.ErrorS(err, "Failed to create target directory")
				return nil, status.Error(codes.Internal, err.Error())
			}
			notMnt = true
		} else {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	// volume mount
	if notMnt {
		mountFlags := req.VolumeCapability.GetMount().GetMountFlags()

		klog.V(5).InfoS("Mounting target",
			"device_path", devicePath,
			"method", "NodeStageVolume",
			"mount_flags", mountFlags,
			"node_id", d.nodeID,
			"node_name", d.nodeName,
			"staging_target_path", target,
			"volume_id", req.VolumeId,
		)
		err := d.mounter.FormatAndMount(devicePath, target, "ext4", mountFlags)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

func (d *Driver) NodeUnstageVolume(_ context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Errorf(codes.InvalidArgument, "volume id not provided")
	}
	if req.StagingTargetPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "staging target path not provided")
	}

	target := req.StagingTargetPath

	klog.V(5).InfoS("Attempting to unmount and clean staging target path",
		"method", "NodeUnstageVolume",
		"node_name", d.nodeName,
		"staging_target_path", target,
		"volume_id", req.VolumeId,
	)
	err := mount.CleanupMountPoint(target, d.mounter, false)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if d.rescanOnResize {
		if err = cloud.RescanSCSIDevices(); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to rescan volume: %s", err)
		}
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (d *Driver) NodePublishVolume(_ context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Errorf(codes.InvalidArgument, "volume id not provided")
	}
	if req.StagingTargetPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "staging target path not provided")
	}
	if req.TargetPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "target path not provided")
	}
	if req.VolumeCapability == nil {
		return nil, status.Errorf(codes.InvalidArgument, "volume capability not provided")
	}

	source := req.StagingTargetPath
	target := req.TargetPath

	klog.V(5).InfoS("Determining if target is not a mount point",
		"method", "NodePublishVolume",
		"node_name", d.nodeName,
		"source", source,
		"target", target,
		"volume_id", req.VolumeId,
	)
	notMnt, err := d.mounter.IsLikelyNotMountPoint(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			err = os.MkdirAll(target, 0750)
			if err != nil {
				klog.ErrorS(err, "Failed to create target directory")
				return nil, status.Error(codes.Internal, err.Error())
			}
			notMnt = true
		} else {
			klog.V(2).ErrorS(err, "IsLikelyNotMountPoint returned error")
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	if notMnt {
		mountFlags := []string{"bind"}
		mountFlags = append(mountFlags, req.VolumeCapability.GetMount().GetMountFlags()...)

		klog.V(5).InfoS("Mounting target",
			"method", "NodePublishVolume",
			"mount_flags", mountFlags,
			"node_name", d.nodeName,
			"source", source,
			"target", target,
			"volume_id", req.VolumeId,
		)
		err := d.mounter.Mount(source, target, "ext4", mountFlags)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (d *Driver) NodeUnpublishVolume(_ context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Errorf(codes.InvalidArgument, "volume id not provided")
	}
	if req.TargetPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "target path not provided")
	}

	klog.V(5).InfoS("Attempting to unmount and clean target path",
		"method", "NodeUnpublishVolume",
		"node_name", d.nodeName,
		"target", req.TargetPath,
		"volume_id", req.VolumeId,
	)
	err := mount.CleanupMountPoint(req.TargetPath, d.mounter, false)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (d *Driver) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	klog.FromContext(ctx).Info("NodeGetVolumeStats called")
	return &csi.NodeGetVolumeStatsResponse{}, nil
}

func (d *Driver) NodeExpandVolume(_ context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Errorf(codes.InvalidArgument, "volume id not provided")
	}

	klog.V(2).InfoS("Expanding volume",
		"method", "NodeExpandVolume",
		"node_id", d.nodeID,
		"node_name", d.nodeName,
		"volume_id", req.VolumeId,
		"volume_path", req.VolumePath,
	)

	devicePath, _, err := mount.GetDeviceNameFromMount(d.mounter, req.VolumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to determine mount path for %s: %s", req.VolumePath, err)
	}

	if d.rescanOnResize {
		if err = cloud.RescanSCSIDevices(); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to rescan volume: %s", err)
		}
	}

	klog.V(5).InfoS("Resizing device path",
		"device_path", devicePath,
		"method", "NodeExpandVolume",
		"volume_path", req.VolumePath,
	)
	r := mount.NewResizeFs(d.mounter.Exec)
	_, err = r.Resize(devicePath, req.VolumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to resize volume: %s", err)
	}

	klog.V(2).InfoS("Expanded volume successfully",
		"device_path", devicePath,
		"method", "NodeExpandVolume",
		"volume_path", req.VolumePath,
	)
	return &csi.NodeExpandVolumeResponse{}, nil
}

func (d *Driver) NodeGetCapabilities(_ context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	var capabilities []*csi.NodeServiceCapability
	for _, capability := range nodeCapabilities {
		capabilities = append(capabilities, &csi.NodeServiceCapability{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: capability,
				},
			},
		})
	}

	klog.V(5).InfoS("Get supported capabilities of the node server",
		"capabilities", capabilities,
		"method", "NodeGetCapabilities",
		"node_id", d.nodeID,
		"node_name", d.nodeName,
		"req", *req,
	)

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: capabilities,
	}, nil
}

func (d *Driver) NodeGetInfo(_ context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	klog.V(5).InfoS("Get info about the current node",
		"method", "NodeGetInfo",
		"node_id", d.nodeID,
		"node_name", d.nodeName,
		"max_volumes_per_node", maxVolumeCountPerNode,
		"req", *req,
	)

	return &csi.NodeGetInfoResponse{
		NodeId:            d.nodeID,
		MaxVolumesPerNode: maxVolumeCountPerNode,
	}, nil
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
		return "", errors.New("device path does not point on a block device")
	}

	return realDevicePath, nil
}
