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

package testclient

import (
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/watch"
)

// FakeTenants implements TenantsInterface. Meant to be embedded into a struct to get a default
// implementation. This makes faking out just the methods you want to test easier.
type FakeTenants struct {
	Fake *Fake
}

func (c *FakeTenants) Get(name string) (*api.Tenant, error) {
	obj, err := c.Fake.Invokes(NewRootGetAction("tenants", name), &api.Tenant{})
	if obj == nil {
		return nil, err
	}

	return obj.(*api.Tenant), err
}

func (c *FakeTenants) List(label labels.Selector, field fields.Selector) (*api.TenantList, error) {
	obj, err := c.Fake.Invokes(NewRootListAction("tenants", label, field), &api.TenantList{})
	if obj == nil {
		return nil, err
	}

	return obj.(*api.TenantList), err
}

func (c *FakeTenants) Create(tenant *api.Tenant) (*api.Tenant, error) {
	obj, err := c.Fake.Invokes(NewRootCreateAction("tenants", tenant), tenant)
	if obj == nil {
		return nil, err
	}

	return obj.(*api.Tenant), err
}

func (c *FakeTenants) Update(tenant *api.Tenant) (*api.Tenant, error) {
	obj, err := c.Fake.Invokes(NewRootUpdateAction("tenants", tenant), tenant)
	if obj == nil {
		return nil, err
	}

	return obj.(*api.Tenant), err
}

func (c *FakeTenants) Delete(name string) error {
	_, err := c.Fake.Invokes(NewRootDeleteAction("tenants", name), &api.Tenant{})
	return err
}

func (c *FakeTenants) Watch(label labels.Selector, field fields.Selector, resourceVersion string) (watch.Interface, error) {
	return c.Fake.InvokesWatch(NewRootWatchAction("tenants", label, field, resourceVersion))
}

func (c *FakeTenants) Finalize(tenant *api.Tenant) (*api.Tenant, error) {
	action := CreateActionImpl{}
	action.Verb = "create"
	action.Resource = "tenants"
	action.Subresource = "finalize"
	action.Object = tenant

	obj, err := c.Fake.Invokes(action, tenant)
	if obj == nil {
		return nil, err
	}

	return obj.(*api.Tenant), err
}

func (c *FakeTenants) Status(tenant *api.Tenant) (*api.Tenant, error) {
	action := CreateActionImpl{}
	action.Verb = "create"
	action.Resource = "tenants"
	action.Subresource = "status"
	action.Object = tenant

	obj, err := c.Fake.Invokes(action, tenant)
	if obj == nil {
		return nil, err
	}

	return obj.(*api.Tenant), err
}
