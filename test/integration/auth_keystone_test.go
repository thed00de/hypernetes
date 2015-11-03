// +build integration,!no-etcd

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

package integration

// This file tests keystone authentication and (soon) authorization of HTTP requests to a master object.
// It does not use the client in pkg/client/... because authentication and authorization needs
// to work for any client of the HTTP interface.

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/testapi"
	"k8s.io/kubernetes/pkg/auth/authenticator"
	"k8s.io/kubernetes/pkg/auth/authorizer"
	"k8s.io/kubernetes/pkg/auth/user"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/master"
	"k8s.io/kubernetes/plugin/pkg/admission/admit"
	"k8s.io/kubernetes/plugin/pkg/auth/authenticator/password/passwordtest"
	"k8s.io/kubernetes/plugin/pkg/auth/authenticator/request/basicauth"
	"k8s.io/kubernetes/test/integration/framework"

	"github.com/golang/glog"
)

var (
	UserAdmin        string = api.UserAdmin
	UserTest         string = "test"
	NamespaceDefault string = api.NamespaceDefault
	NamespaceTest    string = "test"
	TenantDefault    string = api.TenantDefault
	TenantTest       string = "test"
	adminAuthString  string = base64.StdEncoding.EncodeToString([]byte("admin:admin"))
	testAuthString   string = base64.StdEncoding.EncodeToString([]byte("test:test"))
)

func getTestBasicAuth() authenticator.Request {
	passwordAuthenticator := passwordtest.New()
	passwordAuthenticator.Users[UserAdmin] = &user.DefaultInfo{Name: UserAdmin, Password: "admin"}
	passwordAuthenticator.Users[UserTest] = &user.DefaultInfo{Name: UserTest, Password: "test"}
	return basicauth.New(passwordAuthenticator)
}

var testTenant string = `
{
  "kind": "Tenant",
  "apiVersion": "` + testapi.Default.Version() + `",
  "metadata": {
    "name": "` + TenantTest + `"%s
  }
}
`
var testNamespace string = `
{
  "kind": "Namespace",
  "apiVersion": "` + testapi.Default.Version() + `",
  "metadata": {
    "name": "` + NamespaceTest + `",
	"tenant": "` + TenantTest + `"%s
  }
}
`

func getAdminTestRequests() []RequestTerm {
	requests := []RequestTerm{
		// Tenants
		{"POST", path("tenants", "", ""), testTenant, code201},
		{"GET", path("tenants", "", ""), "", code200},
		{"POST", path("namespaces", "", ""), testNamespace, code201},
	}
	comRequests := getCommonTestRequests()
	comDefaultRequests := getTestRequestsWithNamespace(NamespaceDefault)
	comTestRequests := getTestRequestsWithNamespace(NamespaceTest)
	requests = append(requests, comRequests...)
	requests = append(requests, comDefaultRequests...)
	requests = append(requests, comTestRequests...)
	requests = append(requests, RequestTerm{"DELETE", timeoutPath("namespaces", "", NamespaceTest), "", code200})
	requests = append(requests, RequestTerm{"DELETE", timeoutPath("tenants", "", TenantTest), "", code200})
	return requests
}

func getTestRequestsWithAccess() []RequestTerm {
	requests := []RequestTerm{
		{"POST", path("namespaces", "", ""), testNamespace, code201},
		{"GET", path("tenants", "", ""), "", code200},
	}
	comRequests := getCommonTestRequests()
	comTestRequests := getTestRequestsWithNamespace(NamespaceTest)
	requests = append(requests, comRequests...)
	requests = append(requests, comTestRequests...)
	requests = append(requests, RequestTerm{"DELETE", timeoutPath("namespaces", "", NamespaceTest), "", code200})
	return requests
}

func getTestRequestsForibidden() []RequestTerm {
	requests := []RequestTerm{
		// Tenants
		{"POST", path("tenants", "", ""), testTenant, code201},
	}
	comDefaultRequests := getTestRequestsWithNamespace(NamespaceDefault)
	requests = append(requests, comDefaultRequests...)
	requests = append(requests, RequestTerm{"DELETE", timeoutPath("tenants", "", TenantTest), "", code200})
	return requests
}

func splitPath(url string) []string {
	path := strings.Trim(url, "/")
	if path == "" {
		return []string{}
	}
	return strings.Split(url, "/")
}

type allowTestAuthorizer struct {
	kubeClient client.Interface
}

func (ka *allowTestAuthorizer) Authorize(a authorizer.Attributes) (string, error) {
	var (
		tenantName string
		ns         *api.Namespace
		err        error
	)
	if authorizer.IsWhiteListedUser(a.GetUserName()) {
		return "", nil
	} else {
		if !a.IsReadOnly() && a.GetResource() == "tenants" {
			return "", errors.New("only admin can write tenant")
		}
	}
	if a.GetNamespace() != "" {
		ns, err = ka.kubeClient.Namespaces().Get(a.GetNamespace())
		if err != nil {
			glog.Error(err)
			return "", err
		}
		tenantName = ns.Tenant
	} else {
		if a.GetTenant() != "" {
			te, err := ka.kubeClient.Tenants().Get(a.GetTenant())
			if err != nil {
				glog.Error(err)
				return "", err
			}
			tenantName = te.Name
		}
	}
	if tenantName == "" || tenantName == TenantTest {
		return TenantTest, nil
	}
	return "", errors.New("Keystone authorization failed")
}

func TestAdminAccess(t *testing.T) {

	framework.DeleteAllEtcdKeys()

	// Set up a master
	etcdStorage, err := framework.NewEtcdStorage()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	storageDestinations := master.NewStorageDestinations()
	storageDestinations.AddAPIGroup("", etcdStorage)

	var m *master.Master
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		m.Handler.ServeHTTP(w, req)
	}))
	defer s.Close()

	clientConfig := &client.Config{
		Host:     s.URL,
		Version:  testapi.Default.Version(),
		Insecure: false,
		Username: UserAdmin, // since this is a request with auth, so we have to use 'admin' user
		Password: "admin",
	}
	kclient := client.NewOrDie(clientConfig)
	m = master.New(&master.Config{
		StorageDestinations:   storageDestinations,
		KubeletClient:         client.FakeKubeletClient{},
		EnableCoreControllers: true,
		EnableLogsSupport:     false,
		EnableUISupport:       false,
		EnableIndex:           true,
		APIPrefix:             "/api",
		Authenticator:         getTestBasicAuth(),
		Authorizer:            &allowTestAuthorizer{kubeClient: kclient},
		AdmissionControl:      admit.NewAlwaysAdmit(),
		StorageVersions:       map[string]string{"": testapi.Default.Version()},
	})

	previousResourceVersion := make(map[string]float64)
	transport := http.DefaultTransport

	for _, r := range getAdminTestRequests() {
		t.Logf("access %s with %s", r.URL, r.verb)
		var bodyStr string
		if r.body != "" {
			sub := ""
			if r.verb == "PUT" {
				// For update operations, insert previous resource version
				if resVersion := previousResourceVersion[getPreviousResourceVersionKey(r.URL, "")]; resVersion != 0 {
					sub += fmt.Sprintf(",\r\n\"resourceVersion\": \"%v\"", resVersion)
				}
				fields := splitPath(r.URL)
				if len(fields) > 4 && fields[3] == "namespaces" {
					sub += fmt.Sprintf(",\r\n\"namespace\": %q", fields[4])
				} else {
					sub += fmt.Sprintf(",\r\n\"namespace\": %q", NamespaceDefault)
				}
			}
			bodyStr = fmt.Sprintf(r.body, sub)
		}
		r.body = bodyStr
		bodyBytes := bytes.NewReader([]byte(bodyStr))
		req, err := http.NewRequest(r.verb, s.URL+r.URL, bodyBytes)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		req.Header.Set("Authorization", fmt.Sprintf("Basic %s", adminAuthString))
		if r.verb == "PATCH" {
			req.Header.Set("Content-Type", "application/merge-patch+json")
		}

		func() {
			resp, err := transport.RoundTrip(req)
			defer resp.Body.Close()
			if err != nil {
				t.Logf("case %v", r)
				t.Fatalf("unexpected error: %v", err)
			}
			b, _ := ioutil.ReadAll(resp.Body)
			if _, ok := r.statusCodes[resp.StatusCode]; !ok {
				t.Logf("case %v", r)
				t.Errorf("Expected status one of %v, but got %v", r.statusCodes, resp.StatusCode)
				t.Errorf("Body: %v", string(b))
			} else {
				if r.verb == "POST" {
					// For successful create operations, extract resourceVersion
					id, currentResourceVersion, err := parseResourceVersion(b)
					if err == nil {
						key := getPreviousResourceVersionKey(r.URL, id)
						previousResourceVersion[key] = currentResourceVersion
					}
				}
			}

		}()
	}
}

func createTestTenant(url string) error {
	var bodyStr string
	bodyStr = fmt.Sprintf(testTenant, "")
	bodyBytes := bytes.NewReader([]byte(bodyStr))
	req, err := http.NewRequest("POST", url+"/api/v1/tenants", bodyBytes)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Basic %s", adminAuthString))
	transport := http.DefaultTransport
	resp, err := transport.RoundTrip(req)
	defer resp.Body.Close()
	if err != nil {
		return err
	}
	if resp.StatusCode == 201 {
		return nil
	}
	return fmt.Errorf("error, %d", resp.StatusCode)
}

func TestUserTestAccess(t *testing.T) {
	framework.DeleteAllEtcdKeys()

	etcdStorage, err := framework.NewEtcdStorage()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	storageDestinations := master.NewStorageDestinations()
	storageDestinations.AddAPIGroup("", etcdStorage)

	var m *master.Master
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		m.Handler.ServeHTTP(w, req)
	}))
	defer s.Close()

	kubeClient := client.FakeKubeletClient{}
	clientConfig := &client.Config{
		Host:     s.URL,
		Version:  testapi.Default.Version(),
		Insecure: false,
		Username: UserAdmin, // since this is a request with auth, so we have to use 'admin' user
		Password: "admin",
	}
	client := client.NewOrDie(clientConfig)
	m = master.New(&master.Config{
		StorageDestinations:   storageDestinations,
		KubeletClient:         kubeClient,
		EnableCoreControllers: true,
		EnableLogsSupport:     false,
		EnableUISupport:       false,
		EnableIndex:           true,
		APIPrefix:             "/api",
		Authenticator:         getTestBasicAuth(),
		Authorizer:            &allowTestAuthorizer{kubeClient: client},
		AdmissionControl:      admit.NewAlwaysAdmit(),
		StorageVersions:       map[string]string{"": testapi.Default.Version()},
	})

	previousResourceVersion := make(map[string]float64)
	transport := http.DefaultTransport

	for _, r := range getTestRequestsForibidden() {
		t.Logf("access %s with %s", r.URL, r.verb)
		var bodyStr string
		if r.body != "" {
			bodyStr = fmt.Sprintf(r.body, "")
		}
		bodyBytes := bytes.NewReader([]byte(bodyStr))
		req, err := http.NewRequest(r.verb, s.URL+r.URL, bodyBytes)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		req.Header.Set("Authorization", fmt.Sprintf("Basic %s", testAuthString))

		func() {
			resp, err := transport.RoundTrip(req)
			defer resp.Body.Close()
			if err != nil {
				t.Logf("case %v", r)
				t.Fatalf("unexpected error: %v", err)
			}
			// Expect all of bob's actions to return Forbidden
			if resp.StatusCode != http.StatusForbidden {
				t.Logf("case %v", r)
				t.Errorf("Expected not status Forbidden, but got %s", resp.Status)
			}
		}()
	}

	if err := createTestTenant(s.URL); err != nil {
		t.Fatalf("%v", err)
	}
	for _, r := range getTestRequestsWithAccess() {
		t.Logf("access %s with %s", r.URL, r.verb)
		var bodyStr string
		if r.body != "" {
			sub := ""
			if r.verb == "PUT" {
				// For update operations, insert previous resource version
				if resVersion := previousResourceVersion[getPreviousResourceVersionKey(r.URL, "")]; resVersion != 0 {
					sub += fmt.Sprintf(",\r\n\"resourceVersion\": \"%v\"", resVersion)
				}
				sub += fmt.Sprintf(",\r\n\"namespace\": %q", NamespaceTest)
			}
			bodyStr = fmt.Sprintf(r.body, sub)
		}
		r.body = bodyStr
		bodyBytes := bytes.NewReader([]byte(bodyStr))
		req, err := http.NewRequest(r.verb, s.URL+r.URL, bodyBytes)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		req.Header.Set("Authorization", fmt.Sprintf("Basic %s", testAuthString))
		if r.verb == "PATCH" {
			req.Header.Set("Content-Type", "application/merge-patch+json")
		}

		func() {
			resp, err := transport.RoundTrip(req)
			defer resp.Body.Close()
			if err != nil {
				t.Logf("case %v", r)
				t.Fatalf("unexpected error: %v", err)
			}
			b, _ := ioutil.ReadAll(resp.Body)
			if _, ok := r.statusCodes[resp.StatusCode]; !ok {
				t.Logf("case %v", r)
				t.Errorf("Expected status one of %v, but got %v", r.statusCodes, resp.StatusCode)
				t.Errorf("Body: %v", string(b))
			} else {
				if r.verb == "POST" {
					// For successful create operations, extract resourceVersion
					id, currentResourceVersion, err := parseResourceVersion(b)
					if err == nil {
						key := getPreviousResourceVersionKey(r.URL, id)
						previousResourceVersion[key] = currentResourceVersion
					}
				}
			}

		}()
	}
}
