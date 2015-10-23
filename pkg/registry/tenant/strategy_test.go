/*
Copyright 2014 The Kubernetes Authors All rights reserved.

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

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/unversioned"
)

func TestTenantStrategy(t *testing.T) {
	ctx := api.NewDefaultContext()
	if Strategy.AllowCreateOnUpdate() {
		t.Errorf("Tenants should not allow create on update")
	}
	tenant := &api.Tenant{
		ObjectMeta: api.ObjectMeta{Name: "foo", ResourceVersion: "10"},
		Status:     api.TenantStatus{Phase: api.TenantTerminating},
	}
	Strategy.PrepareForCreate(tenant)
	if tenant.Status.Phase != api.TenantActive {
		t.Errorf("Tenants do not allow setting phase on create")
	}
	errs := Strategy.Validate(ctx, tenant)
	if len(errs) != 0 {
		t.Errorf("Unexpected error validating %v", errs)
	}
	invalidTenant := &api.Tenant{
		ObjectMeta: api.ObjectMeta{Name: "bar", ResourceVersion: "4"},
	}
	// ensure we copy spec.finalizers from old to new
	Strategy.PrepareForUpdate(invalidTenant, tenant)
	errs = Strategy.ValidateUpdate(ctx, invalidTenant, tenant)
	if len(errs) == 0 {
		t.Errorf("Expected a validation error")
	}
	if invalidTenant.ResourceVersion != "4" {
		t.Errorf("Incoming resource version on update should not be mutated")
	}
}

func TestTenantStatusStrategy(t *testing.T) {
	ctx := api.NewDefaultContext()
	if StatusStrategy.AllowCreateOnUpdate() {
		t.Errorf("Tenants should not allow create on update")
	}
	now := unversioned.Now()
	oldTenant := &api.Tenant{
		ObjectMeta: api.ObjectMeta{Name: "foo", ResourceVersion: "10"},
		Spec:       api.TenantSpec{},
		Status:     api.TenantStatus{Phase: api.TenantActive},
	}
	tenant := &api.Tenant{
		ObjectMeta: api.ObjectMeta{Name: "foo", ResourceVersion: "9", DeletionTimestamp: &now},
		Status:     api.TenantStatus{Phase: api.TenantTerminating},
	}
	StatusStrategy.PrepareForUpdate(tenant, oldTenant)
	if tenant.Status.Phase != api.TenantTerminating {
		t.Errorf("Tenant status updates should allow change of phase: %v", tenant.Status.Phase)
	}
	errs := StatusStrategy.ValidateUpdate(ctx, tenant, oldTenant)
	if len(errs) != 0 {
		t.Errorf("Unexpected error %v", errs)
	}
	if tenant.ResourceVersion != "9" {
		t.Errorf("Incoming resource version on update should not be mutated")
	}
}
