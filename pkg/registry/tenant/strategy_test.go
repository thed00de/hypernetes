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
	if Strategy.TenantScoped() {
		t.Errorf("Tenants should not be tenant scoped")
	}
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
	if len(tenant.Spec.Finalizers) != 1 || tenant.Spec.Finalizers[0] != api.FinalizerKubernetes {
		t.Errorf("Prepare For Create should have added kubernetes finalizer")
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
	if len(invalidTenant.Spec.Finalizers) != 1 || invalidTenant.Spec.Finalizers[0] != api.FinalizerKubernetes {
		t.Errorf("PrepareForUpdate should have preserved old.spec.finalizers")
	}
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
	if StatusStrategy.TenantScoped() {
		t.Errorf("Tenants should not be tenant scoped")
	}
	if StatusStrategy.AllowCreateOnUpdate() {
		t.Errorf("Tenants should not allow create on update")
	}
	now := unversioned.Now()
	oldTenant := &api.Tenant{
		ObjectMeta: api.ObjectMeta{Name: "foo", ResourceVersion: "10"},
		Spec:       api.TenantSpec{Finalizers: []api.FinalizerName{"kubernetes"}},
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
	if len(tenant.Spec.Finalizers) != 1 || tenant.Spec.Finalizers[0] != api.FinalizerKubernetes {
		t.Errorf("PrepareForUpdate should have preserved old finalizers")
	}
	errs := StatusStrategy.ValidateUpdate(ctx, tenant, oldTenant)
	if len(errs) != 0 {
		t.Errorf("Unexpected error %v", errs)
	}
	if tenant.ResourceVersion != "9" {
		t.Errorf("Incoming resource version on update should not be mutated")
	}
}

func TestTenantFinalizeStrategy(t *testing.T) {
	ctx := api.NewDefaultContext()
	if FinalizeStrategy.TenantScoped() {
		t.Errorf("Tenants should not be tenant scoped")
	}
	if FinalizeStrategy.AllowCreateOnUpdate() {
		t.Errorf("Tenants should not allow create on update")
	}
	oldTenant := &api.Tenant{
		ObjectMeta: api.ObjectMeta{Name: "foo", ResourceVersion: "10"},
		Spec:       api.TenantSpec{Finalizers: []api.FinalizerName{"kubernetes", "example.com/org"}},
		Status:     api.TenantStatus{Phase: api.TenantActive},
	}
	tenant := &api.Tenant{
		ObjectMeta: api.ObjectMeta{Name: "foo", ResourceVersion: "9"},
		Spec:       api.TenantSpec{Finalizers: []api.FinalizerName{"example.com/foo"}},
		Status:     api.TenantStatus{Phase: api.TenantTerminating},
	}
	FinalizeStrategy.PrepareForUpdate(tenant, oldTenant)
	if tenant.Status.Phase != api.TenantActive {
		t.Errorf("finalize updates should not allow change of phase: %v", tenant.Status.Phase)
	}
	if len(tenant.Spec.Finalizers) != 1 || string(tenant.Spec.Finalizers[0]) != "example.com/foo" {
		t.Errorf("PrepareForUpdate should have modified finalizers")
	}
	errs := StatusStrategy.ValidateUpdate(ctx, tenant, oldTenant)
	if len(errs) != 0 {
		t.Errorf("Unexpected error %v", errs)
	}
	if tenant.ResourceVersion != "9" {
		t.Errorf("Incoming resource version on update should not be mutated")
	}
}
