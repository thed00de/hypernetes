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
	"fmt"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/errors"
	"k8s.io/kubernetes/pkg/api/unversioned"
	"k8s.io/kubernetes/pkg/client/cache"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/controller/framework"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/util"
	"k8s.io/kubernetes/pkg/watch"

	"github.com/golang/glog"
)

// TenantController is responsible for performing actions dependent upon a tenant phase
type TenantController struct {
	controller     *framework.Controller
	StopEverything chan struct{}
}

// NewTenantController creates a new TenantController
func NewTenantController(kubeClient client.Interface, versions *unversioned.APIVersions, resyncPeriod time.Duration) *TenantController {
	var controller *framework.Controller
	_, controller = framework.NewInformer(
		&cache.ListWatch{
			ListFunc: func() (runtime.Object, error) {
				return kubeClient.Tenants().List(labels.Everything(), fields.Everything())
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return kubeClient.Tenants().Watch(labels.Everything(), fields.Everything(), options)
			},
		},
		&api.Tenant{},
		// TODO: Can we have much longer period here?
		resyncPeriod,
		framework.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				tenant := obj.(*api.Tenant)
				if err := syncTenant(kubeClient, versions, tenant); err != nil {
					if estimate, ok := err.(*contentRemainingError); ok {
						go func() {
							// Estimate is the aggregate total of TerminationGracePeriodSeconds, which defaults to 30s
							// for pods.  However, most processes will terminate faster - within a few seconds, probably
							// with a peak within 5-10s.  So this division is a heuristic that avoids waiting the full
							// duration when in many cases things complete more quickly. The extra second added is to
							// ensure we never wait 0 seconds.
							t := estimate.Estimate/2 + 1
							glog.V(4).Infof("Content remaining in tenant %s, waiting %d seconds", tenant.Name, t)
							time.Sleep(time.Duration(t) * time.Second)
							if err := controller.Requeue(tenant); err != nil {
								util.HandleError(err)
							}
						}()
						return
					}
					util.HandleError(err)
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				tenant := newObj.(*api.Tenant)
				if err := syncTenant(kubeClient, versions, tenant); err != nil {
					if estimate, ok := err.(*contentRemainingError); ok {
						go func() {
							t := estimate.Estimate/2 + 1
							glog.V(4).Infof("Content remaining in tenant %s, waiting %d seconds", tenant.Name, t)
							time.Sleep(time.Duration(t) * time.Second)
							if err := controller.Requeue(tenant); err != nil {
								util.HandleError(err)
							}
						}()
						return
					}
					util.HandleError(err)
				}
			},
		},
	)

	return &TenantController{
		controller: controller,
	}
}

// Run begins observing the system.  It starts a goroutine and returns immediately.
func (nm *TenantController) Run() {
	if nm.StopEverything == nil {
		nm.StopEverything = make(chan struct{})
		go nm.controller.Run(nm.StopEverything)
	}
}

// Stop gracefully shutsdown this controller
func (nm *TenantController) Stop() {
	if nm.StopEverything != nil {
		close(nm.StopEverything)
		nm.StopEverything = nil
	}
}

// deleteAllContent will delete all content known to the system in a tenant. It returns an estimate
// of the time remaining before the remaining resources are deleted. If estimate > 0 not all resources
// are guaranteed to be gone.
func deleteAllContent(kubeClient client.Interface, versions *unversioned.APIVersions, tenant string, before unversioned.Time) (estimate int64, err error) {
	items, err := kubeClient.Namespaces().List(labels.Everything(), fields.Everything())
	if err != nil {
		return estimate, err
	}
	for _, namespace := range items.Items {
		if namespace.Tenant != tenant {
			continue
		}
		if err = kubeClient.Namespaces().Delete(namespace.Name); err != nil {
			return estimate, err
		}
	}
	return estimate, nil
}

type contentRemainingError struct {
	Estimate int64
}

func (e *contentRemainingError) Error() string {
	return fmt.Sprintf("some content remains in the tenant, estimate %d seconds before it is removed", e.Estimate)
}

// syncTenant orchestrates deletion of a Tenant and its associated content.
func syncTenant(kubeClient client.Interface, versions *unversioned.APIVersions, tenant *api.Tenant) (err error) {
	if tenant.DeletionTimestamp == nil {
		return nil
	}

	// there may still be content for us to remove
	estimate, err := deleteAllContent(kubeClient, versions, tenant.Name, *tenant.DeletionTimestamp)
	if err != nil {
		return err
	}
	if estimate > 0 {
		return &contentRemainingError{estimate}
	}

	err = kubeClient.Tenants().Delete(tenant.Name)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}
