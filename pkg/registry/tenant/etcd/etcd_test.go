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

package etcd

import (
	"testing"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/testapi"
	"k8s.io/kubernetes/pkg/api/unversioned"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/registry/registrytest"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/tools"
	"k8s.io/kubernetes/pkg/tools/etcdtest"
)

func newStorage(t *testing.T) (*REST, *tools.FakeEtcdClient) {
	etcdStorage, fakeClient := registrytest.NewEtcdStorage(t, "")
	storage, _ := NewREST(etcdStorage)
	return storage, fakeClient
}

func validNewTenant() *api.Tenant {
	return &api.Tenant{
		ObjectMeta: api.ObjectMeta{
			Name: "foo",
		},
	}
}

func TestCreate(t *testing.T) {
	storage, fakeClient := newStorage(t)
	test := registrytest.New(t, fakeClient, storage.Etcd).ClusterScope()
	tenant := validNewTenant()
	tenant.ObjectMeta = api.ObjectMeta{GenerateName: "foo"}
	test.TestCreate(
		// valid
		tenant,
		// invalid
		&api.Tenant{
			ObjectMeta: api.ObjectMeta{Name: "bad value"},
		},
	)
}

func TestCreateSetsFields(t *testing.T) {
	storage, _ := newStorage(t)
	tenant := validNewTenant()
	ctx := api.NewContext()
	_, err := storage.Create(ctx, tenant)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	object, err := storage.Get(ctx, "foo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	actual := object.(*api.Tenant)
	if actual.Name != tenant.Name {
		t.Errorf("unexpected tenant: %#v", actual)
	}
	if len(actual.UID) == 0 {
		t.Errorf("expected tenant UID to be set: %#v", actual)
	}
	if actual.Status.Phase != api.TenantActive {
		t.Errorf("expected tenant phase to be set to active, but %v", actual.Status.Phase)
	}
}

func TestDelete(t *testing.T) {
	storage, fakeClient := newStorage(t)
	test := registrytest.New(t, fakeClient, storage.Etcd).ClusterScope().ReturnDeletedObject()
	test.TestDelete(validNewTenant())
}

func TestGet(t *testing.T) {
	storage, fakeClient := newStorage(t)
	test := registrytest.New(t, fakeClient, storage.Etcd).ClusterScope()
	test.TestGet(validNewTenant())
}

func TestList(t *testing.T) {
	storage, fakeClient := newStorage(t)
	test := registrytest.New(t, fakeClient, storage.Etcd).ClusterScope()
	test.TestList(validNewTenant())
}

func TestWatch(t *testing.T) {
	storage, fakeClient := newStorage(t)
	test := registrytest.New(t, fakeClient, storage.Etcd).ClusterScope()
	test.TestWatch(
		validNewTenant(),
		// matching labels
		[]labels.Set{},
		// not matching labels
		[]labels.Set{
			{"foo": "bar"},
		},
		// matching fields
		[]fields.Set{
			{"metadata.name": "foo"},
			{"name": "foo"},
		},
		// not matching fields
		[]fields.Set{
			{"metadata.name": "bar"},
		},
	)
}

func TestDeleteTenant(t *testing.T) {
	storage, fakeClient := newStorage(t)
	key := etcdtest.AddPrefix("tenants/foo")
	ctx := api.NewContext()
	now := unversioned.Now()
	tenant := &api.Tenant{
		ObjectMeta: api.ObjectMeta{
			Name:              "foo",
			DeletionTimestamp: &now,
		},
		Spec:   api.TenantSpec{},
		Status: api.TenantStatus{Phase: api.TenantActive},
	}
	if _, err := fakeClient.Set(key, runtime.EncodeOrDie(testapi.Default.Codec(), tenant), 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := storage.Delete(ctx, "foo", nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
