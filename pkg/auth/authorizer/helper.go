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

package authorizer

import (
	"strings"

	"k8s.io/kubernetes/pkg/api"
)

func IsWhiteListedUser(username string) bool {
	if strings.HasPrefix(username, "system:serviceaccount:") {
		return true
	}
	whiteList := map[string]bool{
		api.UserAdmin:               true,
		"kubelet":                   true,
		"kube_proxy":                true,
		"system:scheduler":          true,
		"system:controller_manager": true,
		"system:logging":            true,
		"system:monitoring":         true,
	}
	return whiteList[username]
}
