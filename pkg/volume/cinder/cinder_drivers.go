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
	"github.com/golang/glog"
	"sync"
)

// Driver interface
type DriverInterface interface {
	Attach(volumeData map[string]interface{}, globalPDPath string) error
	Detach(volumeData map[string]interface{}, globalPDPath string) error
	Format(volumeData map[string]interface{}, fstype string) error
}

// Factory is a function that returns a DriverInterface
type Factory func() (DriverInterface, error)

// All registered cloud drivers.
var driversMutex sync.Mutex
var drivers = make(map[string]Factory)

// RegisterDriver registers cinder volume drivers by name
func RegisterCinderDriver(name string, cloud Factory) {
	driversMutex.Lock()
	defer driversMutex.Unlock()
	if _, found := drivers[name]; found {
		glog.Fatalf("Driver %q was registered twice", name)
	}
	glog.V(1).Infof("Registered driver %q", name)
	drivers[name] = cloud
}

// Get driver by name
func GetCinderDriver(name string) (DriverInterface, error) {
	driversMutex.Lock()
	defer driversMutex.Unlock()
	f, found := drivers[name]
	if !found {
		return nil, nil
	}
	return f()
}
