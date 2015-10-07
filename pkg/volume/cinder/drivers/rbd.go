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

package drivers

import (
	"encoding/json"

	"github.com/golang/glog"
	"k8s.io/kubernetes/pkg/volume/cinder"
)

const (
	DriverName = "rbd"
)

type RBDDriver struct {
}

type RBDVolume struct {
	keyring     string   `json:"keyring"`
	authEnabled bool     `json:"auth_enabled"`
	authUser    string   `json:"auth_username"`
	hosts       []string `json:"hosts"`
	ports       []int    `json:"ports"`
	name        string   `json:"name"`
	accessMode  bool     `json:"access_mode"`
}

func newRBDDriver() (cinder.DriverInterface, error) {
	return &RBDDriver{}, nil
}

func init() {
	cinder.RegisterCinderDriver(DriverName, func() (cinder.DriverInterface, error) { return newRBDDriver() })
}

func (d *RBDDriver) toRBDVolume(volumeData map[string]interface{}) (*RBDVolume, error) {
	data, err := json.Marshal(volumeData)
	if err != nil {
		return nil, err
	}

	var volume RBDVolume
	err = json.Unmarshal(data, &volume)
	if err != nil {
		return nil, err
	}

	return &volume, nil
}

func (d *RBDDriver) Attach(volumeData map[string]interface{}, globalPDPath string) error {
	volume, err := d.toRBDVolume(volumeData)
	if err != nil {
		return err
	}

	glog.V(4).Infof("Attach cinder rbd %v to %s", volume, globalPDPath)
	return nil
}

func (d *RBDDriver) Detach(volumeData map[string]interface{}, globalPDPath string) error {
	volume, err := d.toRBDVolume(volumeData)
	if err != nil {
		return err
	}

	glog.V(4).Infof("Attach cinder rbd %v to %s", volume, globalPDPath)
	return nil
}
