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

package unversioned

import (
	"fmt"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/watch"
)

type TenantsInterface interface {
	Tenants() TenantInterface
}

type TenantInterface interface {
	Create(item *api.Tenant) (*api.Tenant, error)
	Get(name string) (result *api.Tenant, err error)
	List(label labels.Selector, field fields.Selector) (*api.TenantList, error)
	Delete(name string) error
	Update(item *api.Tenant) (*api.Tenant, error)
	Watch(label labels.Selector, field fields.Selector, resourceVersion string) (watch.Interface, error)
	Finalize(item *api.Tenant) (*api.Tenant, error)
	Status(item *api.Tenant) (*api.Tenant, error)
}

// tenants implements TenantsInterface
type tenants struct {
	r *Client
}

// newTenants returns a tenants object.
func newTenants(c *Client) *tenants {
	return &tenants{r: c}
}

// Create creates a new tenant.
func (c *tenants) Create(tenant *api.Tenant) (*api.Tenant, error) {
	result := &api.Tenant{}
	err := c.r.Post().Resource("tenants").Body(tenant).Do().Into(result)
	return result, err
}

// List lists all the tenants in the cluster.
func (c *tenants) List(label labels.Selector, field fields.Selector) (*api.TenantList, error) {
	result := &api.TenantList{}
	err := c.r.Get().
		Resource("tenants").
		LabelsSelectorParam(label).
		FieldsSelectorParam(field).
		Do().Into(result)
	return result, err
}

// Update takes the representation of a tenant to update.  Returns the server's representation of the tenant, and an error, if it occurs.
func (c *tenants) Update(tenant *api.Tenant) (result *api.Tenant, err error) {
	result = &api.Tenant{}
	if len(tenant.ResourceVersion) == 0 {
		err = fmt.Errorf("invalid update object, missing resource version: %v", tenant)
		return
	}
	err = c.r.Put().Resource("tenants").Name(tenant.Name).Body(tenant).Do().Into(result)
	return
}

// Finalize takes the representation of a tenant to update.  Returns the server's representation of the tenant, and an error, if it occurs.
func (c *tenants) Finalize(tenant *api.Tenant) (result *api.Tenant, err error) {
	result = &api.Tenant{}
	if len(tenant.ResourceVersion) == 0 {
		err = fmt.Errorf("invalid update object, missing resource version: %v", tenant)
		return
	}
	err = c.r.Put().Resource("tenants").Name(tenant.Name).SubResource("finalize").Body(tenant).Do().Into(result)
	return
}

// Status takes the representation of a tenant to update.  Returns the server's representation of the tenant, and an error, if it occurs.
func (c *tenants) Status(tenant *api.Tenant) (result *api.Tenant, err error) {
	result = &api.Tenant{}
	if len(tenant.ResourceVersion) == 0 {
		err = fmt.Errorf("invalid update object, missing resource version: %v", tenant)
		return
	}
	err = c.r.Put().Resource("tenants").Name(tenant.Name).SubResource("status").Body(tenant).Do().Into(result)
	return
}

// Get gets an existing tenant
func (c *tenants) Get(name string) (*api.Tenant, error) {
	result := &api.Tenant{}
	err := c.r.Get().Resource("tenants").Name(name).Do().Into(result)
	return result, err
}

// Delete deletes an existing tenant.
func (c *tenants) Delete(name string) error {
	return c.r.Delete().Resource("tenants").Name(name).Do().Error()
}

// Watch returns a watch.Interface that watches the requested tenants.
func (c *tenants) Watch(label labels.Selector, field fields.Selector, resourceVersion string) (watch.Interface, error) {
	return c.r.Get().
		Prefix("watch").
		Resource("tenants").
		Param("resourceVersion", resourceVersion).
		LabelsSelectorParam(label).
		FieldsSelectorParam(field).
		Watch()
}
