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
	"fmt"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/validation"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/registry/generic"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/util/fielderrors"
)

// tenantStrategy implements behavior for Tenants
type tenantStrategy struct {
	runtime.ObjectTyper
	api.NameGenerator
}

// Strategy is the default logic that applies when creating and updating Tenant
// objects via the REST API.
var Strategy = tenantStrategy{api.Scheme, api.SimpleNameGenerator}

// NamespaceScoped is false for Tenant
func (tenantStrategy) NamespaceScoped() bool {
	return false
}

// PrepareForCreate clears fields that are not allowed to be set by end users on creation.
func (tenantStrategy) PrepareForCreate(obj runtime.Object) {
	// on create, status is active
	tenant := obj.(*api.Tenant)
	tenant.Status = api.TenantStatus{
		Phase: api.TenantActive,
	}
}

// PrepareForUpdate clears fields that are not allowed to be set by end users on update.
func (tenantStrategy) PrepareForUpdate(obj, old runtime.Object) {
	newTenant := obj.(*api.Tenant)
	oldTenant := old.(*api.Tenant)
	newTenant.Status = oldTenant.Status
}

// Validate validates a new tenant.
func (tenantStrategy) Validate(ctx api.Context, obj runtime.Object) fielderrors.ValidationErrorList {
	tenant := obj.(*api.Tenant)
	return validation.ValidateTenant(tenant)
}

// AllowCreateOnUpdate is false for tenants.
func (tenantStrategy) AllowCreateOnUpdate() bool {
	return false
}

// ValidateUpdate is the default update validation for an end user.
func (tenantStrategy) ValidateUpdate(ctx api.Context, obj, old runtime.Object) fielderrors.ValidationErrorList {
	errorList := validation.ValidateTenant(obj.(*api.Tenant))
	return append(errorList, validation.ValidateTenantUpdate(obj.(*api.Tenant), old.(*api.Tenant))...)
}

func (tenantStrategy) AllowUnconditionalUpdate() bool {
	return true
}

type tenantStatusStrategy struct {
	tenantStrategy
}

var StatusStrategy = tenantStatusStrategy{Strategy}

func (tenantStatusStrategy) PrepareForUpdate(obj, old runtime.Object) {
	newTenant := obj.(*api.Tenant)
	oldTenant := old.(*api.Tenant)
	newTenant.Spec = oldTenant.Spec
}

func (tenantStatusStrategy) ValidateUpdate(ctx api.Context, obj, old runtime.Object) fielderrors.ValidationErrorList {
	return validation.ValidateTenantStatusUpdate(obj.(*api.Tenant), old.(*api.Tenant))
}

// MatchTenant returns a generic matcher for a given label and field selector.
func MatchTenant(label labels.Selector, field fields.Selector) generic.Matcher {
	return generic.MatcherFunc(func(obj runtime.Object) (bool, error) {
		tenantObj, ok := obj.(*api.Tenant)
		if !ok {
			return false, fmt.Errorf("not a tenant")
		}
		fields := TenantToSelectableFields(tenantObj)
		return label.Matches(labels.Set(tenantObj.Labels)) && field.Matches(fields), nil
	})
}

// TenantToSelectableFields returns a label set that represents the object
func TenantToSelectableFields(tenant *api.Tenant) labels.Set {
	return labels.Set{
		"metadata.name": tenant.Name,
		"status.phase":  string(tenant.Status.Phase),
		// This is a bug, but we need to support it for backward compatibility.
		"name": tenant.Name,
	}
}
