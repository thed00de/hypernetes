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
	"k8s.io/kubernetes/pkg/api/unversioned"
	"k8s.io/kubernetes/pkg/registry/generic"
	"k8s.io/kubernetes/pkg/registry/network"
	"k8s.io/kubernetes/pkg/registry/registrytest"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/storage/etcd/etcdtest"
	etcdtesting "k8s.io/kubernetes/pkg/storage/etcd/testing"
)

func newStorage(t *testing.T) (*REST, *etcdtesting.EtcdTestServer) {
	etcdStorage, server := registrytest.NewEtcdStorage(t, "")
	storage, _ := NewREST(etcdStorage, generic.UndecoratedStorage)
	return storage, server
}

func validNewNetwork() *api.Network {
	return &api.Network{
		ObjectMeta: api.ObjectMeta{
			Name: "foo",
		},
		Spec: api.NetworkSpec{
			TenantID: "123",
			Subnets: map[string]api.Subnet{
				"subnet1": {
					CIDR:    "192.168.0.0/24",
					Gateway: "192.168.0.1",
				},
			},
		},
	}
}

func TestStorage(t *testing.T) {
	storage, _ := newStorage(t)
	network.NewRegistry(storage)
}

func TestCreate(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := registrytest.New(t, storage.Etcd).ClusterScope()
	Network := validNewNetwork()
	Network.ObjectMeta = api.ObjectMeta{GenerateName: "foo"}
	test.TestCreate(
		// valid
		Network,
		// invalid
		&api.Network{
			ObjectMeta: api.ObjectMeta{Name: "bad value"},
		},
	)
}

func expectNetwork(t *testing.T, out runtime.Object) (*api.Network, bool) {
	Network, ok := out.(*api.Network)
	if !ok || Network == nil {
		t.Errorf("Expected an api.Network object, was %#v", out)
		return nil, false
	}
	return Network, true
}

func TestCreateSetsFields(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	Network := validNewNetwork()
	ctx := api.NewContext()
	_, err := storage.Create(ctx, Network)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	object, err := storage.Get(ctx, "foo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	actual := object.(*api.Network)
	if actual.Name != Network.Name {
		t.Errorf("unexpected Network: %#v", actual)
	}
	if len(actual.UID) == 0 {
		t.Errorf("expected Network UID to be set: %#v", actual)
	}
	if actual.Status.Phase != api.NetworkInitializing {
		t.Errorf("expected Network phase to be set to initializing, but %v", actual.Status.Phase)
	}
}

func TestGet(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := registrytest.New(t, storage.Etcd).ClusterScope()
	test.TestGet(validNewNetwork())
}

func TestList(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := registrytest.New(t, storage.Etcd).ClusterScope()
	test.TestList(validNewNetwork())
}

func TestDeleteNetwork(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	key := etcdtest.AddPrefix("networks/foo")
	ctx := api.NewContext()
	now := unversioned.Now()
	net := &api.Network{
		ObjectMeta: api.ObjectMeta{
			Name:              "foo",
			DeletionTimestamp: &now,
		},
		Spec: api.NetworkSpec{
			TenantID: "123",
			Subnets: map[string]api.Subnet{
				"subnet1": {
					CIDR:    "192.168.0.0/24",
					Gateway: "192.168.0.1",
				},
			},
		},
		Status: api.NetworkStatus{Phase: api.NetworkInitializing},
	}
	if err := storage.Storage.Set(ctx, key, net, nil, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := storage.Delete(ctx, "foo", nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
