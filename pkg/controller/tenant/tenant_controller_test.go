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

package tenant

import (
	"testing"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/unversioned"
	"k8s.io/kubernetes/pkg/client/unversioned/testclient"
)

func TestSyncTenantThatIsActive(t *testing.T) {
	mockClient := &testclient.Fake{}
	testTenant := &api.Tenant{
		ObjectMeta: api.ObjectMeta{
			Name:            "test",
			ResourceVersion: "1",
		},
		Status: api.TenantStatus{
			Phase: api.TenantActive,
		},
	}
	err := syncTenant(mockClient, &unversioned.APIVersions{}, testTenant)
	if err != nil {
		t.Errorf("Unexpected error when synching tenant %v", err)
	}
}

func TestRunStop(t *testing.T) {
	mockClient := &testclient.Fake{}

	teController := NewTenantController(mockClient, &unversioned.APIVersions{}, 1*time.Second)

	if teController.StopEverything != nil {
		t.Errorf("Non-running manager should not have a stop channel.  Got %v", teController.StopEverything)
	}

	teController.Run()

	if teController.StopEverything == nil {
		t.Errorf("Running manager should have a stop channel.  Got nil")
	}

	teController.Stop()

	if teController.StopEverything != nil {
		t.Errorf("Non-running manager should not have a stop channel.  Got %v", teController.StopEverything)
	}
}
