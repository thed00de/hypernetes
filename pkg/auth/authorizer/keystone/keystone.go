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

package keystone

import (
	"errors"
	"strings"

	"k8s.io/kubernetes/pkg/auth/authorizer"
	client "k8s.io/kubernetes/pkg/client/unversioned"

	"github.com/golang/glog"
	"github.com/rackspace/gophercloud"
	"github.com/rackspace/gophercloud/openstack"
	"github.com/rackspace/gophercloud/openstack/identity/v2/tenants"
	"github.com/rackspace/gophercloud/openstack/identity/v2/users"
	"github.com/rackspace/gophercloud/pagination"
)

type authConfig struct {
	AuthUrl  string `json:"auth-url"`
	Username string `json:"user-name"`
	Password string `json:"password"`
	TokenID  string `json:"token"`
	Tenant   string `json:"tenant"`
}

type OpenstackClient struct {
	provider   *gophercloud.ProviderClient
	authClient *gophercloud.ServiceClient
	config     *authConfig
}

type keystoneAuthorizer struct {
	kubeClient client.Interface
	osClient   OpenstackInterface
}

func newOpenstackClient(config *authConfig) (*OpenstackClient, error) {

	if config == nil {
		err := errors.New("no OpenStack cloud provider config file given")
		return nil, err
	}

	opts := gophercloud.AuthOptions{
		IdentityEndpoint: config.AuthUrl,
		Username:         config.Username,
		Password:         config.Password,
		TenantName:       config.Tenant,
		//TokenID:     config.TokenID,
		AllowReauth: true,
	}

	provider, err := openstack.AuthenticatedClient(opts)
	if err != nil {
		glog.Info("Failed: Starting openstack authenticate client")
		return nil, err
	}
	authClient := openstack.NewIdentityV2(provider)

	return &OpenstackClient{
		provider,
		authClient,
		config,
	}, nil
}

func NewKeystoneAuthorizer(kubeClient client.Interface) (*keystoneAuthorizer, error) {

	ka := &keystoneAuthorizer{
		kubeClient: kubeClient,
	}
	return ka, nil
}

// Authorizer implements authorizer.Authorize
func (ka *keystoneAuthorizer) Authorize(a authorizer.Attributes) error {

	var (
		tenantID string
		userID   string
	)
	if strings.HasPrefix(a.GetUserName(), "system:serviceaccount:") {
		return nil
	}
	if isWhiteListedUser(a.GetUserName()) {
		return nil
	}
	ns, err := ka.kubeClient.Namespaces().Get(a.GetNamespace())
	if err != nil {
		return err
	}
	authConfig := &authConfig{
		AuthUrl:  "http://127.0.0.1:35357/v2.0",
		Username: a.GetUserName(),
		Password: a.GetPassword(),
		TokenID:  a.GetToken(),
		Tenant:   ns.Tenant,
	}
	osClient, err1 := newOpenstackClient(authConfig)
	if err1 != nil {
		glog.Errorf("%v", err1)
		return err1
	}
	tenantID, err = osClient.getTenantID(ns.Tenant)
	if err != nil {
		glog.Errorf("%v", err)
		return err
	}
	userID, err = osClient.getUserID(a.GetUserName())
	if err != nil {
		glog.Errorf("%v", err)
		return err
	}
	hasRole, err := osClient.roleCheck(userID, tenantID)
	if err != nil {
		glog.V(4).Infof("Keystone authorization failed: %v", err)
		return errors.New("Keystone authorization failed")
	}
	if hasRole {
		return nil
	} else {
		return errors.New("User not authorized through keystone for namespace")
	}
	return errors.New("Keystone authorization failed")
}

// Checks if a user has access to a tenant
func (osClient *OpenstackClient) roleCheck(userID string, tenantID string) (bool, error) {
	if userID == "" {
		return false, errors.New("UserID null during authorization")
	}
	if tenantID == "" {
		return false, errors.New("UserID null during authorization")
	}
	hasRole := false
	pager := users.ListRoles(osClient.authClient, tenantID, userID)
	err := pager.EachPage(func(page pagination.Page) (bool, error) {
		roleList, err := users.ExtractRoles(page)
		if err != nil {
			return false, err
		}
		if len(roleList) > 0 {
			hasRole = true
		}
		return true, nil
	})

	if err != nil {
		return false, err
	}
	return hasRole, nil
}

func isWhiteListedUser(username string) bool {
	whiteList := map[string]bool{
		"kubelet":                   true,
		"kube_proxy":                true,
		"system:scheduler":          true,
		"system:controller_manager": true,
		"system:logging":            true,
		"system:monitoring":         true,
	}
	return whiteList[username]
}

func (osClient *OpenstackClient) getTenantID(name string) (id string, err error) {
	tenantList := make([]tenants.Tenant, 0)
	opts := tenants.ListOpts{}
	pager := tenants.List(osClient.authClient, &opts)
	err = pager.EachPage(func(page pagination.Page) (bool, error) {
		tenantList, err = tenants.ExtractTenants(page)
		if err != nil {
			return false, err
		}
		return true, nil
	})
	if err != nil {
		return "", err
	}
	for _, t := range tenantList {
		if name == t.Name {
			return t.ID, nil
		}
	}
	return "", nil
}

func (osClient *OpenstackClient) getUserID(name string) (id string, err error) {
	userList := make([]users.User, 0)
	pager := users.List(osClient.authClient)
	err = pager.EachPage(func(page pagination.Page) (bool, error) {
		userList, err = users.ExtractUsers(page)
		if err != nil {
			return false, err
		}
		return true, nil
	})
	if err != nil {
		return "", err
	}
	for _, u := range userList {
		if name == u.Name {
			return u.ID, nil
		}
	}
	return "", nil
}
