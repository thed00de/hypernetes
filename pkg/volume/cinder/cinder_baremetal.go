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
	"errors"
	"io/ioutil"
	"strings"

	"github.com/golang/glog"
	"github.com/rackspace/gophercloud/openstack/blockstorage/v2/extensions/volumeactions"
)

const (
	defaultMountPoint = "xvda"
)

type CinderBaremetalUtil struct {
	client   *cinderClient
	hostname string
}

func (cb *CinderBaremetalUtil) AttachDiskBaremetal(b *cinderVolumeBuilder, globalPDPath string) error {
	volume, err := cb.client.getVolume(b.pdName)
	if err != nil {
		return err
	}

	if volume.Status != "available" {
		return errors.New("Volume is not available")
	}

	mountMode := "rw"
	if b.readOnly {
		mountMode = "ro"
	}

	// attach volume
	attachOpts := volumeactions.AttachOpts{
		MountPoint: defaultMountPoint,
		Mode:       mountMode,
		HostName:   cb.hostname,
	}

	err = cb.client.attach(volume.ID, attachOpts)
	if err != nil {
		return err
	}

	connectionInfo, err := cb.client.getConnectionInfo(volume.ID, cb.getConnectionOptions())
	if err != nil {
		return err
	}

	glog.V(4).Infof("Get cinder connection info %v", connectionInfo)

	volumeType := connectionInfo["driver_volume_type"].(string)
	cinderDriver, err := GetCinderDriver(volumeType)
	if err != nil {
		glog.Warningf("Get cinder driver %s failed: %v", volumeType, err)
		return err
	}

	data := connectionInfo["data"].(map[string]interface{})
	if volumeType == "rbd" {
		data["keyring"] = cb.client.keyring
	}

	err = cinderDriver.Attach(data, globalPDPath)
	if err != nil {
		return err
	}

	return nil
}

// Unmounts the device and detaches the disk from the kubelet's host machine.
func (cb *CinderBaremetalUtil) DetachDiskBaremetal(cd *cinderVolumeCleaner, globalPDPath string) error {
	volume, err := cb.client.getVolume(cd.pdName)
	if err != nil {
		return err
	}

	connectionInfo, err := cb.client.getConnectionInfo(volume.ID, cb.getConnectionOptions())
	if err != nil {
		return err
	}

	glog.V(4).Infof("Get cinder connection info %v", connectionInfo)

	volumeType := connectionInfo["driver_volume_type"].(string)
	cinderDriver, err := GetCinderDriver(volumeType)
	if err != nil {
		glog.Warningf("Get cinder driver %s failed: %v", volumeType, err)
		return err
	}
	data := connectionInfo["data"].(map[string]interface{})
	if volumeType == "rbd" {
		data["keyring"] = cb.client.keyring
	}

	err = cinderDriver.Detach(data, globalPDPath)
	if err != nil {
		return err
	}

	err = cb.client.detach(volume.ID, cb.getConnectionOptions())
	if err != nil {
		return err
	}

	return nil
}

// Get iscsi initiator
func (cb *CinderBaremetalUtil) getIscsiInitiator() string {
	contents, err := ioutil.ReadFile("/etc/iscsi/initiatorname.iscsi")
	if err != nil {
		return ""
	}

	lines := strings.Split(string(contents), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "InitiatorName=") {
			return strings.Split(line, "=")[1]
		}
	}

	return ""
}

// Get cinder connections options
func (cb *CinderBaremetalUtil) getConnectionOptions() *volumeactions.ConnectorOpts {
	connector := volumeactions.ConnectorOpts{
		Host:      cb.hostname,
		Initiator: cb.getIscsiInitiator(),
	}

	return &connector
}
