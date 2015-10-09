/*
Copyright 2015 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cinder

import (
	"io/ioutil"
	"os"
	"path"

	"github.com/golang/glog"
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/types"
	"k8s.io/kubernetes/pkg/util"
	"k8s.io/kubernetes/pkg/util/exec"
	"k8s.io/kubernetes/pkg/util/mount"
	"k8s.io/kubernetes/pkg/volume"
)

const (
	OpenStackCloudProviderTagFile = "OpenStackCloudProviderTagFile"
)

// This is the primary entrypoint for volume plugins.
func ProbeVolumePlugins() []volume.VolumePlugin {
	return []volume.VolumePlugin{&cinderPlugin{nil}}
}

type cinderPlugin struct {
	host volume.VolumeHost
}

var _ volume.VolumePlugin = &cinderPlugin{}

const (
	cinderVolumePluginName = "kubernetes.io/cinder"
)

func (plugin *cinderPlugin) Init(host volume.VolumeHost) {
	plugin.host = host
}

func (plugin *cinderPlugin) Name() string {
	return cinderVolumePluginName
}

func (plugin *cinderPlugin) CanSupport(spec *volume.Spec) bool {
	return (spec.Volume != nil && spec.Volume.Cinder != nil) || (spec.PersistentVolume != nil && spec.PersistentVolume.Spec.Cinder != nil)
}

func (plugin *cinderPlugin) GetAccessModes() []api.PersistentVolumeAccessMode {
	return []api.PersistentVolumeAccessMode{
		api.ReadWriteOnce,
	}
}

func (plugin *cinderPlugin) NewBuilder(spec *volume.Spec, pod *api.Pod, _ volume.VolumeOptions) (volume.Builder, error) {
	return plugin.newBuilderInternal(spec, pod.UID, newCinderDiskUtil(plugin.host.GetCinderConfig(),
		plugin.host.IsNoMountSupported()), plugin.host.GetMounter(), plugin.host.IsNoMountSupported())
}

func (plugin *cinderPlugin) newBuilderInternal(spec *volume.Spec, podUID types.UID, manager cdManager, mounter mount.Interface, isNoMountSupported bool) (volume.Builder, error) {
	var cinder *api.CinderVolumeSource
	if spec.Volume != nil && spec.Volume.Cinder != nil {
		cinder = spec.Volume.Cinder
	} else {
		cinder = spec.PersistentVolume.Spec.Cinder
	}

	pdName := cinder.VolumeID
	fsType := cinder.FSType
	readOnly := cinder.ReadOnly

	return &cinderVolumeBuilder{
		cinderVolume: &cinderVolume{
			podUID:  podUID,
			volName: spec.Name(),
			pdName:  pdName,
			mounter: mounter,
			manager: manager,
			plugin:  plugin,
		},
		fsType:             fsType,
		readOnly:           readOnly,
		withOpenStackCP:    cinder.WithOpenStackCP,
		isNoMountSupported: isNoMountSupported,
		blockDeviceMounter: &cinderSafeFormatAndMount{mounter, exec.New()}}, nil
}

func (plugin *cinderPlugin) NewCleaner(volName string, podUID types.UID) (volume.Cleaner, error) {
	return plugin.newCleanerInternal(volName, podUID, newCinderDiskUtil(plugin.host.GetCinderConfig(),
		plugin.host.IsNoMountSupported()), plugin.host.GetMounter(), plugin.host.IsNoMountSupported())
}

func (plugin *cinderPlugin) newCleanerInternal(volName string, podUID types.UID, manager cdManager, mounter mount.Interface, isNoMountSupported bool) (volume.Cleaner, error) {
	clearner := cinderVolumeCleaner{
		&cinderVolume{
			podUID:  podUID,
			volName: volName,
			manager: manager,
			mounter: mounter,
			plugin:  plugin,
		},
		false,
		isNoMountSupported,
	}

	return &clearner, nil
}

// Abstract interface to PD operations.
type cdManager interface {
	// Attaches the disk to the kubelet's host machine.
	AttachDisk(builder *cinderVolumeBuilder, globalPDPath string) error
	// Detaches the disk from the kubelet's host machine.
	DetachDisk(cleaner *cinderVolumeCleaner) error
}

var _ volume.Builder = &cinderVolumeBuilder{}

type cinderVolumeBuilder struct {
	*cinderVolume
	fsType             string
	readOnly           bool
	withOpenStackCP    bool
	isNoMountSupported bool
	blockDeviceMounter mount.Interface
}

// cinderPersistentDisk volumes are disk resources provided by C3
// that are attached to the kubelet's host machine and exposed to the pod.
type cinderVolume struct {
	volName string
	podUID  types.UID
	// Unique identifier of the volume, used to find the disk resource in the provider.
	pdName string
	// Filesystem type, optional.
	fsType string
	// Specifies the partition to mount
	//partition string
	// Specifies whether the disk will be attached as read-only.
	readOnly bool
	// Utility interface that provides API calls to the provider to attach/detach disks.
	manager cdManager
	// Mounter interface that provides system calls to mount the global path to the pod local path.
	mounter mount.Interface
	// diskMounter provides the interface that is used to mount the actual block device.
	blockDeviceMounter mount.Interface
	// metadata provides meta of the volume
	metadata map[string]interface{}
	plugin   *cinderPlugin
}

func detachDiskLogError(cd *cinderVolume, isNoMountSupported bool) {
	err := cd.manager.DetachDisk(&cinderVolumeCleaner{cd, true, isNoMountSupported})
	if err != nil {
		glog.Warningf("Failed to detach disk: %v (%v)", cd, err)
	}
}

func (b *cinderVolumeBuilder) SetUp() error {
	return b.SetUpAt(b.GetPath())
}

// SetUp attaches the disk and bind mounts to the volume path.
func (b *cinderVolumeBuilder) SetUpAt(dir string) error {
	// TODO: handle failed mounts here.
	notmnt, err := b.mounter.IsLikelyNotMountPoint(dir)
	glog.V(4).Infof("PersistentDisk set up: %s %v %v", dir, !notmnt, err)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if !notmnt {
		return nil
	}
	globalPDPath := makeGlobalPDName(b.plugin.host, b.pdName)
	if err := b.manager.AttachDisk(b, globalPDPath); err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0750); err != nil {
		// TODO: we should really eject the attach/detach out into its own control loop.
		detachDiskLogError(b.cinderVolume, b.isNoMountSupported)
		return err
	}

	// Without openstack cloud provider, create a tag file
	if !b.withOpenStackCP && b.isNoMountSupported {
		fw, err := os.Create(path.Join(dir, OpenStackCloudProviderTagFile))
		if err != nil {
			os.Remove(dir)
			detachDiskLogError(b.cinderVolume, b.isNoMountSupported)
			return err
		}

		_, err = fw.WriteString(b.pdName)
		if err != nil {
			os.Remove(dir)
			detachDiskLogError(b.cinderVolume, b.isNoMountSupported)
			return err
		}

		fw.Close()
		return nil
	}

	options := []string{"bind"}
	if b.readOnly {
		options = append(options, "ro")
	}

	// Perform a bind mount to the full path to allow duplicate mounts of the same PD.
	err = b.mounter.Mount(globalPDPath, dir, "", options)
	if err != nil {
		notmnt, mntErr := b.mounter.IsLikelyNotMountPoint(dir)
		if mntErr != nil {
			glog.Errorf("IsLikelyNotMountPoint check failed: %v", mntErr)
			return err
		}
		if !notmnt {
			if mntErr = b.mounter.Unmount(dir); mntErr != nil {
				glog.Errorf("Failed to unmount: %v", mntErr)
				return err
			}
			notmnt, mntErr := b.mounter.IsLikelyNotMountPoint(dir)
			if mntErr != nil {
				glog.Errorf("IsLikelyNotMountPoint check failed: %v", mntErr)
				return err
			}
			if !notmnt {
				// This is very odd, we don't expect it.  We'll try again next sync loop.
				glog.Errorf("%s is still mounted, despite call to unmount().  Will try again next sync loop.", b.GetPath())
				return err
			}
		}
		os.Remove(dir)
		// TODO: we should really eject the attach/detach out into its own control loop.
		detachDiskLogError(b.cinderVolume, b.isNoMountSupported)
		return err
	}

	return nil
}

func (b *cinderVolumeBuilder) IsReadOnly() bool {
	return b.readOnly
}

func makeGlobalPDName(host volume.VolumeHost, devName string) string {
	return path.Join(host.GetPluginDir(cinderVolumePluginName), "mounts", devName)
}

func (cd *cinderVolume) GetPath() string {
	name := cinderVolumePluginName
	return cd.plugin.host.GetPodVolumeDir(cd.podUID, util.EscapeQualifiedNameForDisk(name), cd.volName)
}

// GetMetadata returns the metadata of the volume
func (cd *cinderVolume) GetMetaData() map[string]interface{} {
	return cd.metadata
}

type cinderVolumeCleaner struct {
	*cinderVolume
	withOpenStackCP    bool
	isNoMountSupported bool
}

var _ volume.Cleaner = &cinderVolumeCleaner{}

func (c *cinderVolumeCleaner) TearDown() error {
	return c.TearDownAt(c.GetPath())
}

// Unmounts the bind mount, and detaches the disk only if the PD
// resource was the last reference to that disk on the kubelet.
func (c *cinderVolumeCleaner) TearDownAt(dir string) error {
	notmnt, err := c.mounter.IsLikelyNotMountPoint(dir)
	if err != nil {
		return err
	}

	exist, _ := util.FileExists(path.Join(dir, OpenStackCloudProviderTagFile))
	if exist {
		c.withOpenStackCP = false
	} else {
		c.withOpenStackCP = true
	}

	if notmnt {
		if !c.withOpenStackCP && c.isNoMountSupported {
			volumeID, err := ioutil.ReadFile(path.Join(dir, OpenStackCloudProviderTagFile))
			if err != nil {
				return err
			}

			c.pdName = string(volumeID)
			if err := c.manager.DetachDisk(c); err != nil {
				return err
			}
		}

		return os.RemoveAll(dir)
	}

	refs, err := mount.GetMountRefs(c.mounter, dir)
	if err != nil {
		return err
	}
	if err := c.mounter.Unmount(dir); err != nil {
		return err
	}
	glog.Infof("successfully unmounted: %s\n", dir)

	// If refCount is 1, then all bind mounts have been removed, and the
	// remaining reference is the global mount. It is safe to detach.
	if len(refs) == 1 {
		c.pdName = path.Base(refs[0])
		if err := c.manager.DetachDisk(c); err != nil {
			return err
		}
	}
	notmnt, mntErr := c.mounter.IsLikelyNotMountPoint(dir)
	if mntErr != nil {
		glog.Errorf("IsLikelyNotMountPoint check failed: %v", mntErr)
		return err
	}
	if !notmnt {
		if err := os.Remove(dir); err != nil {
			return err
		}
	}
	return nil
}
