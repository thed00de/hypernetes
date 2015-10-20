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

package apiserver

import (
	"errors"
	"fmt"

	"k8s.io/kubernetes/pkg/auth/authorizer"
	"k8s.io/kubernetes/pkg/auth/authorizer/abac"
	"k8s.io/kubernetes/pkg/auth/authorizer/keystone"
	"k8s.io/kubernetes/pkg/auth/authorizer/union"
	client "k8s.io/kubernetes/pkg/client/unversioned"
)

// Attributes implements authorizer.Attributes interface.
type Attributes struct {
	// TODO: add fields and methods when authorizer.Attributes is completed.
}

// alwaysAllowAuthorizer is an implementation of authorizer.Attributes
// which always says yes to an authorization request.
// It is useful in tests and when using kubernetes in an open manner.
type alwaysAllowAuthorizer struct{}

func (alwaysAllowAuthorizer) Authorize(a authorizer.Attributes) (tenant string, err error) {
	return "", nil
}

func NewAlwaysAllowAuthorizer() authorizer.Authorizer {
	return new(alwaysAllowAuthorizer)
}

// alwaysDenyAuthorizer is an implementation of authorizer.Attributes
// which always says no to an authorization request.
// It is useful in unit tests to force an operation to be forbidden.
type alwaysDenyAuthorizer struct{}

func (alwaysDenyAuthorizer) Authorize(a authorizer.Attributes) (tenant string, err error) {
	return "", errors.New("Everything is forbidden.")
}

func NewAlwaysDenyAuthorizer() authorizer.Authorizer {
	return new(alwaysDenyAuthorizer)
}

const (
	ModeAlwaysAllow string = "AlwaysAllow"
	ModeAlwaysDeny  string = "AlwaysDeny"
	ModeABAC        string = "ABAC"
	ModeKeystone    string = "Keystone"
)

// Keep this list in sync with constant list above.
var AuthorizationModeChoices = []string{ModeAlwaysAllow, ModeAlwaysDeny, ModeABAC, ModeKeystone}

type AuthorizerConfig struct {
	AuthorizationModes      []string
	AuthorizationPolicyFile string
	KubeClient              client.Interface
	KeystonAuthURL          string
}

// NewAuthorizerFromAuthorizationConfig returns the right sort of union of multiple authorizer.Authorizer objects
// based on the authorizationMode or an error.  authorizationMode should be a comma separated values
// of AuthorizationModeChoices.
func NewAuthorizerFromAuthorizationConfig(authzConfig AuthorizerConfig) (authorizer.Authorizer, error) {

	if len(authzConfig.AuthorizationModes) == 0 {
		return nil, errors.New("Atleast one authorization mode should be passed")
	}

	var authorizers []authorizer.Authorizer
	authorizerMap := make(map[string]bool)

	for _, authorizationMode := range authzConfig.AuthorizationModes {
		if authorizerMap[authorizationMode] {
			return nil, fmt.Errorf("Authorization mode %s specified more than once", authorizationMode)
		}
		// Keep cases in sync with constant list above.
		switch authorizationMode {
		case ModeAlwaysAllow:
			authorizers = append(authorizers, NewAlwaysAllowAuthorizer())
		case ModeAlwaysDeny:
			authorizers = append(authorizers, NewAlwaysDenyAuthorizer())
		case ModeABAC:
			if authzConfig.AuthorizationPolicyFile == "" {
				return nil, errors.New("ABAC's authorization policy file not passed")
			}
			abacAuthorizer, err := abac.NewFromFile(authzConfig.AuthorizationPolicyFile)
			if err != nil {
				return nil, err
			}
			authorizers = append(authorizers, abacAuthorizer)
		case ModeKeystone:
			if authzConfig.KeystonAuthURL == "" {
				return nil, errors.New("Cannot use mode Keystone without specifying --experimental-keystone-url")
			}
			keystoneAuthorizer, err := keystone.NewKeystoneAuthorizer(authzConfig.KubeClient, authzConfig.KeystonAuthURL)
			if err != nil {
				return nil, err
			}
			authorizers = append(authorizers, keystoneAuthorizer)
		default:
			return nil, fmt.Errorf("Unknown authorization mode %s specified", authorizationMode)
		}
		authorizerMap[authorizationMode] = true
	}

	if !authorizerMap[ModeABAC] && authzConfig.AuthorizationPolicyFile != "" {
		return nil, errors.New("Cannot specify --authorization-policy-file without mode ABAC")
	}
	if !authorizerMap[ModeKeystone] && authzConfig.KeystonAuthURL != "" {
		return nil, errors.New("Cannot specify --experimental-keystone-url without mode Keystone")
	}

	return union.New(authorizers...), nil
}
