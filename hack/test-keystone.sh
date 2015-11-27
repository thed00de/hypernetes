#!/bin/bash

# Copyright 2015 The Kubernetes Authors All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This command checks that the built commands can function together for
# simple scenarios.  It does not require Docker so it can run in travis.

set -o errexit
set -o nounset
set -o pipefail

KUBE_ROOT=$(dirname "${BASH_SOURCE}")/..
source "${KUBE_ROOT}/hack/lib/init.sh"
source "${KUBE_ROOT}/hack/lib/test.sh"

# Stops the running kubectl proxy, if there is one.
function stop-proxy()
{
  [[ -n "${PROXY_PID-}" ]] && sudo -E kill "${PROXY_PID}" 1>&2 2>/dev/null
  [[ -n "${SCHEDULER_PID-}" ]] && sudo -E kill "${SCHEDULER_PID}" 1>&2 2>/dev/null
  PROXY_PID=
  SCHEDULER_PID=
}

# Starts "kubect proxy" to test the client proxy. $1: api_prefix
function start-proxy()
{
  stop-proxy

  kube::log::status "Starting kubectl proxy"

  sudo -E "${KUBE_OUTPUT_HOSTBIN}/kube-proxy" \
    --master="http://${API_HOST}:${API_PORT}" 2>&1 &

  PROXY_PID=$!
  if [ $# -eq 0 ]; then
    kube::util::wait_for_url "http://${API_HOST}:${API_PORT}/healthz" "kubectl proxy"
  else
    kube::util::wait_for_url "http://${API_HOST}:${API_PORT}/$1/healthz" "kubectl proxy --api-prefix=$1"
  fi

  sudo -E "${KUBE_OUTPUT_HOSTBIN}/kube-scheduler" \
    --master="http://${API_HOST}:${API_PORT}" 2>&1 &
  SCHEDULER_PID=$!
}

function cleanup()
{
  [[ -n "${APISERVER_PID-}" ]] && sudo -E kill "${APISERVER_PID}" 1>&2 2>/dev/null
  [[ -n "${CTLRMGR_PID-}" ]] && sudo -E kill "${CTLRMGR_PID}" 1>&2 2>/dev/null
  [[ -n "${KUBELET_PID-}" ]] && sudo -E kill "${KUBELET_PID}" 1>&2 2>/dev/null
  stop-proxy

  kube::etcd::cleanup
  kube::keystone::cleanup
  sudo -E rm -rf "${KUBE_TEMP}"

  kube::log::status "Clean up complete"
}

# Executes curl against the proxy. $1 is the path to use, $2 is the desired
# return code. Prints a helpful message on failure.
function check-curl-proxy-code()
{
  local status
  local -r address=$1
  local -r desired=$2
  local -r full_address="${API_HOST}:${API_PORT}${address}"
  status=$(curl -w "%{http_code}" --silent --output /dev/null "${full_address}")
  if [ "${status}" == "${desired}" ]; then
    return 0
  fi
  echo "For address ${full_address}, got ${status} but wanted ${desired}"
  return 1
}

# TODO: Remove this function when we do the retry inside the kubectl commands. See #15333.
function kubectl-with-retry()
{
  ERROR_FILE="${KUBE_TEMP}/kubectl-error"
  for count in $(seq 0 3); do
    kubectl "$@" 2> ${ERROR_FILE} || true
    if grep -q "the object has been modified" "${ERROR_FILE}"; then
      kube::log::status "retry $1, error: $(cat ${ERROR_FILE})"
      rm "${ERROR_FILE}"
      sleep $((2**count))
    else
      rm "${ERROR_FILE}"
      break
    fi
  done
}

kube::util::trap_add cleanup EXIT

kube::util::ensure-temp-dir
kube::etcd::start
kube::keystone::start

ETCD_HOST=${ETCD_HOST:-127.0.0.1}
ETCD_PORT=${ETCD_PORT:-4001}
KEYSTONE_PORT=${KEYSTONE_PORT:-5000}
API_PORT=${API_PORT:-8080}
SSL_API_PORT=${SSL_API_PORT:-6443}
API_HOST=${API_HOST:-127.0.0.1}
KUBELET_PORT=${KUBELET_PORT:-10250}
KUBELET_HEALTHZ_PORT=${KUBELET_HEALTHZ_PORT:-10248}
CTLRMGR_PORT=${CTLRMGR_PORT:-10252}
ADMIN_USERNAME=${ADMIN_USERNAME:-"admin"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"admin"}
TEST_USERNAME=${TEST_USERNAME:-"test"}
TEST_PASSWORD=${TEST_PASSWORD:-"test"}
ALLOW_SECURITY_CONTEXT=${ALLOW_SECURITY_CONTEXT:-""}

# ensure ~/.kube/config isn't loaded by tests
HOME="${KUBE_TEMP}"
# This is the default dir and filename where the apiserver will generate a self-signed cert
# which should be able to be used as the CA to verify itself
CERT_DIR=$KUBE_TEMP/kubernetes
ROOT_CA_FILE=$CERT_DIR/apiserver.crt

# Check kubectl
kube::log::status "Running kubectl with no options"
"${KUBE_OUTPUT_HOSTBIN}/kubectl"

SERVICE_ACCOUNT_LOOKUP=${SERVICE_ACCOUNT_LOOKUP:-false}
SERVICE_ACCOUNT_KEY=${SERVICE_ACCOUNT_KEY:-"/tmp/kube-serviceaccount.key"}
# Generate ServiceAccount key if needed
if [[ ! -f "${SERVICE_ACCOUNT_KEY}" ]]; then
  mkdir -p "$(dirname ${SERVICE_ACCOUNT_KEY})"
  openssl genrsa -out "${SERVICE_ACCOUNT_KEY}" 2048 2>/dev/null
fi

# Start kube-apiserver
# Admission Controllers to invoke prior to persisting objects in cluster
if [[ -z "${ALLOW_SECURITY_CONTEXT}" ]]; then
  ADMISSION_CONTROL=NamespaceLifecycle,NamespaceAutoProvision,LimitRanger,SecurityContextDeny,ServiceAccount,ResourceQuota
else
  ADMISSION_CONTROL=NamespaceLifecycle,NamespaceAutoProvision,LimitRanger,ServiceAccount,ResourceQuota
fi

kube::log::status "Starting kube-apiserver"
sudo -E "${KUBE_OUTPUT_HOSTBIN}/kube-apiserver" \
  --cert-dir="${CERT_DIR}" \
  --service-account-key-file="${SERVICE_ACCOUNT_KEY}" \
  --service-account-lookup="${SERVICE_ACCOUNT_LOOKUP}" \
  --admission-control="${ADMISSION_CONTROL}" \
  --insecure-bind-address="${API_HOST}" \
  --insecure-port="${API_PORT}" \
  --secure-port="${SSL_API_PORT}" \
  --etcd-servers="http://${ETCD_HOST}:${ETCD_PORT}" \
  --runtime-config=api/v1 \
  --runtime_config="extensions/v1beta1=true" \
  --authorization-mode="Keystone" \
  --experimental-keystone-url="http://127.0.0.1:5000/v2.0" \
  --service-cluster-ip-range="10.0.0.0/24" 2>&1 &
APISERVER_PID=$!

echo "Waiting for apiserver to come up"
kube::util::wait_for_url "http://127.0.0.1:${API_PORT}/healthz" "apiserver"

# Start controller manager
kube::log::status "Starting controller-manager"
sudo -E "${KUBE_OUTPUT_HOSTBIN}/kube-controller-manager" \
  --service-account-private-key-file="${SERVICE_ACCOUNT_KEY}" \
  --root-ca-file="${ROOT_CA_FILE}" \
  --port="${CTLRMGR_PORT}" \
  --master="${API_HOST}:${API_PORT}" 1>&2 &
CTLRMGR_PID=$!

kube::util::wait_for_url "http://127.0.0.1:${CTLRMGR_PORT}/healthz" "controller-manager"

kube::log::status "Starting kubelet in masterless mode"
sudo -E "${KUBE_OUTPUT_HOSTBIN}/kubelet" \
  --really-crash-for-testing=true \
  --root-dir=/tmp/kubelet.$$ \
  --cert-dir="${CERT_DIR}" \
  --docker-endpoint="fake://" \
  --hostname-override="127.0.0.1" \
  --address="127.0.0.1" \
  --port="$KUBELET_PORT" \
  --healthz-port="${KUBELET_HEALTHZ_PORT}" 1>&2 &
KUBELET_PID=$!
kube::util::wait_for_url "http://${API_HOST}:${KUBELET_HEALTHZ_PORT}/healthz" "kubelet(masterless)"
sudo -E kill ${KUBELET_PID} 1>&2 2>/dev/null

kube::log::status "Starting kubelet in masterful mode"
sudo -E "${KUBE_OUTPUT_HOSTBIN}/kubelet" \
  --really-crash-for-testing=true \
  --root-dir=/tmp/kubelet.$$ \
  --cert-dir="${CERT_DIR}" \
  --docker-endpoint="fake://" \
  --hostname-override="127.0.0.1" \
  --address="127.0.0.1" \
  --api-servers="${API_HOST}:${API_PORT}" \
  --port="$KUBELET_PORT" \
  --healthz-port="${KUBELET_HEALTHZ_PORT}" 1>&2 &
KUBELET_PID=$!

kube::util::wait_for_url "http://${API_HOST}:${KUBELET_HEALTHZ_PORT}/healthz" "kubelet"

kube::util::wait_for_url "http://127.0.0.1:${API_PORT}/api/v1/nodes/127.0.0.1" "apiserver(nodes)"

# Expose kubectl directly for readability
PATH="${KUBE_OUTPUT_HOSTBIN}":$PATH

runTests() {
  version="$1"
  echo "Testing api version: $1"
  if [[ -z "${version}" ]]; then
    kube_flags_insecure=(
      -s "http://${API_HOST}:${API_PORT}"
      --match-server-version
    )
    kube_admin_flags=(
      -s "https://${API_HOST}:${SSL_API_PORT}"
      --match-server-version
      --insecure-skip-tls-verify=true
      --username=${ADMIN_USERNAME}
      --password=${ADMIN_PASSWORD}
    )
    kube_user_flags=(
      -s "https://${API_HOST}:${SSL_API_PORT}"
      --match-server-version
      --insecure-skip-tls-verify=true
      --username=${TEST_USERNAME}
      --password=${TEST_PASSWORD}
    )
    [ "$(kubectl get nodes -o go-template='{{ .apiVersion }}' ${kube_admin_flags[@]})" == "v1" ]
  else
    kube_flags_insecure=(
      -s "http://${API_HOST}:${API_PORT}"
      --match-server-version
    )
    kube_admin_flags=(
      -s "https://${API_HOST}:${SSL_API_PORT}"
      --match-server-version
      --insecure-skip-tls-verify=true
      --username=${ADMIN_USERNAME}
      --password=${ADMIN_PASSWORD}
      --api-version="${version}"
    )
    kube_user_flags=(
      -s "https://${API_HOST}:${SSL_API_PORT}"
      --match-server-version
      --insecure-skip-tls-verify=true
      --username=${TEST_USERNAME}
      --password=${TEST_PASSWORD}
      --api-version="${version}"
    )
    echo "kubectl get nodes ${kube_admin_flags[@]}"
    [ "$(kubectl get nodes -o go-template='{{ .apiVersion }}' ${kube_admin_flags[@]})" == "${version}" ]
  fi
  id_field=".metadata.name"
  labels_field=".metadata.labels"
  annotations_field=".metadata.annotations"
  service_selector_field=".spec.selector"
  rc_replicas_field=".spec.replicas"
  rc_status_replicas_field=".status.replicas"
  rc_container_image_field=".spec.template.spec.containers"
  port_field="(index .spec.ports 0).port"
  port_name="(index .spec.ports 0).name"
  second_port_field="(index .spec.ports 1).port"
  second_port_name="(index .spec.ports 1).name"
  image_field="(index .spec.containers 0).image"
  hpa_min_field=".spec.minReplicas"
  hpa_max_field=".spec.maxReplicas"
  hpa_cpu_field=".spec.cpuUtilization.targetPercentage"

  # Passing no arguments to create is an error
  ! kubectl create

  #######################
  # kubectl local proxy #
  #######################

  # Make sure the UI can be proxied
  start-proxy

  ##############################
  # tenant creation / deletion #
  ##############################
  kube::log::status "Testing kubectl(${version}:tenants)"

  kube_flags=${kube_user_flags[@]}
  kube::test::get_object_assert "tenants" "{{range.items}}{{$id_field}}:{{end}}" ''
  ! kubectl create -f docs/user-guide/te.yaml "${kube_user_flags[@]}"

  kube_flags=${kube_admin_flags[@]}
  kube::test::get_object_assert "tenants" "{{range.items}}{{$id_field}}:{{end}}" 'default:'
  kubectl create -f docs/user-guide/te.yaml "${kube_admin_flags[@]}"
  kube::test::get_object_assert "tenants" "{{range.items}}{{$id_field}}:{{end}}" 'default:test:'

  kube_flags=${kube_user_flags[@]}
  kube::test::get_object_assert "tenants" "{{range.items}}{{$id_field}}:{{end}}" 'test:'

  ! kubectl delete tenant test "${kube_user_flags[@]}" --grace-period=0
  kube::test::get_object_assert "tenants" "{{range.items}}{{$id_field}}:{{end}}" 'test:'

  kubectl delete tenant test "${kube_admin_flags[@]}" --grace-period=0
  # https://github.com/hyperhq/hypernetes/issues/52
  # kube::test::get_object_assert "tenants test" "{{.status.phase}}" 'Terminating'
  kube::test::get_object_assert "tenants" "{{range.items}}{{$id_field}}:{{end}}" ''

  kubectl create -f docs/user-guide/te.yaml "${kube_admin_flags[@]}"
  kube::test::get_object_assert "tenants" "{{range.items}}{{$id_field}}:{{end}}" 'test:'
  kubectl create -f docs/user-guide/ns.yaml "${kube_user_flags[@]}"
  kube::test::get_object_assert "namespaces" "{{range.items}}{{$id_field}}:{{end}}" 'test:'

  kubectl delete tenant test "${kube_admin_flags[@]}" --grace-period=0
  sleep 5
  kube::test::get_object_assert "tenants" "{{range.items}}{{$id_field}}:{{end}}" ''
  kube::test::get_object_assert "namespaces" "{{range.items}}{{$id_field}}:{{end}}" ''

  #################################
  # namespace creation / deletion #
  #################################
  kube::log::status "Testing kubectl(${version}:namespaces)"

  kube_flags=${kube_admin_flags[@]}
  kube::test::get_object_assert "namespaces" "{{range.items}}{{$id_field}}:{{end}}" 'default:'
  kube_flags=${kube_user_flags[@]}
  kube::test::get_object_assert "namespaces" "{{range.items}}{{$id_field}}:{{end}}" ''

  # Create 'test' tenant
  kubectl create -f docs/user-guide/te.yaml "${kube_admin_flags[@]}"
  # Create 'test' namespace for 'test' tenant
  kubectl create -f docs/user-guide/ns.yaml "${kube_user_flags[@]}"
  kube::test::get_object_assert "namespaces" "{{range.items}}{{$id_field}}:{{end}}" 'test:'
  kube_flags=${kube_admin_flags[@]}
  kube::test::get_object_assert "namespaces" "{{range.items}}{{$id_field}}:{{end}}" 'default:test:'
  ! kubectl delete namespace default "${kube_user_flags[@]}" --grace-period=0
  kube::test::get_object_assert "namespaces" "{{range.items}}{{$id_field}}:{{end}}" 'default:test:'
  kubectl delete namespace test "${kube_user_flags[@]}" --grace-period=0
  sleep 5
  kube::test::get_object_assert "namespaces" "{{range.items}}{{$id_field}}:{{end}}" 'default:'

  ###########################
  # POD creation / deletion #
  ###########################
  kube::log::status "Testing kubectl(${version}:pods)"

  kube_flags=${kube_admin_flags[@]}
  ### Create POD valid-pod from JSON
  # Pre-condition: no POD is running
  kube::test::get_object_assert "pods" "{{range.items}}{{$id_field}}:{{end}}" ''
  # Create 'test' namespace for 'test' tenant
  kubectl create -f docs/user-guide/ns.yaml "${kube_user_flags[@]}"
  kube::test::get_object_assert "namespaces" "{{range.items}}{{$id_field}}:{{end}}" 'default:test:'
  # should specific the namespace
  ! kubectl create -f docs/admin/limitrange/valid-pod.yaml "${kube_user_flags[@]}"
  kube::test::get_object_assert "pods" "{{range.items}}{{$id_field}}:{{end}}" ''
  kubectl create -f docs/admin/limitrange/valid-pod.yaml "${kube_user_flags[@]}" --namespace=test
  kube::test::get_object_assert "pods" "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'
  kube::test::get_object_assert 'pod valid-pod --namespace=test' "{{$id_field}}" 'valid-pod'
  kube::test::get_object_assert 'pod/valid-pod --namespace=test' "{{$id_field}}" 'valid-pod'
  kube::test::get_object_assert 'pods/valid-pod --namespace=test' "{{$id_field}}" 'valid-pod'
  # Repeat above test using jsonpath template
  kube::test::get_object_jsonpath_assert pods "{.items[*]$id_field}" 'valid-pod'
  kube::test::get_object_jsonpath_assert 'pod valid-pod --namespace=test' "{$id_field}" 'valid-pod'
  kube::test::get_object_jsonpath_assert 'pod/valid-pod --namespace=test' "{$id_field}" 'valid-pod'
  kube::test::get_object_jsonpath_assert 'pods/valid-pod --namespace=test' "{$id_field}" 'valid-pod'
  # Describe command should print detailed information
  kube::test::describe_object_assert pods 'valid-pod --namespace=test' "Name:" "Image(s):" "Node:" "Labels:" "Status:" "Replication Controllers"
  # Describe command (resource only) should print detailed information
  kube::test::describe_resource_assert pods "Name:" "Image(s):" "Node:" "Labels:" "Status:" "Replication Controllers"

  ### Dump current valid-pod POD
  output_pod=$(kubectl get pod valid-pod --namespace=test -o yaml --output-version=v1 "${kube_user_flags[@]}")

  ### Delete POD valid-pod by id
  # Pre-condition: valid-pod POD is running
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'
  # Command
  kubectl delete pod valid-pod --namespace=test "${kube_user_flags[@]}" --grace-period=0
  # Post-condition: no POD is running
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''

  ### Create POD valid-pod from dumped YAML
  # Pre-condition: no POD is running
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  echo "${output_pod}" | kubectl create -f - "${kube_user_flags[@]}" --namespace=test
  # Post-condition: valid-pod POD is running
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'

  ### Delete POD valid-pod from JSON
  # Pre-condition: valid-pod POD is running
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'
  # Command
  kubectl delete -f docs/admin/limitrange/valid-pod.yaml "${kube_user_flags[@]}" --grace-period=0 --namespace=test
  # Post-condition: no POD is running
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''

  ### Create POD redis-master from JSON
  # Pre-condition: no POD is running
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f docs/admin/limitrange/valid-pod.yaml "${kube_user_flags[@]}" --namespace=test
  # Post-condition: valid-pod POD is running
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'

  ### Delete POD valid-pod with label
  # Pre-condition: valid-pod POD is running
  kube::test::get_object_assert "pods -l'name in (valid-pod)'" '{{range.items}}{{$id_field}}:{{end}}' 'valid-pod:'
  # Command
  kubectl delete pods -l'name in (valid-pod)' "${kube_user_flags[@]}" --grace-period=0 --namespace=test
  # Post-condition: no POD is running
  kube::test::get_object_assert "pods -l'name in (valid-pod)'" '{{range.items}}{{$id_field}}:{{end}}' ''

  ### Create POD in 'default' namespace, test user can not access it
  # Pre-condition: no POD is running
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  ! kubectl create -f docs/admin/limitrange/valid-pod.yaml "${kube_user_flags[@]}" --namespace=default
  kubectl create -f docs/admin/limitrange/valid-pod.yaml "${kube_admin_flags[@]}" --namespace=default
  # Post-condition: valid-pod POD is running
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'
  kube::test::get_object_assert 'pod valid-pod --namespace=default' "{{$id_field}}" 'valid-pod'
  kube::test::get_object_assert 'pod/valid-pod --namespace=default' "{{$id_field}}" 'valid-pod'
  kube::test::get_object_assert 'pods/valid-pod --namespace=default' "{{$id_field}}" 'valid-pod'
  kube_flags=${kube_user_flags[@]}
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  ! kubectl delete pod valid-pod "${kube_user_flags[@]}"
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  ! kubectl delete pods -lnew-name=new-valid-pod --grace-period=0 "${kube_user_flags[@]}"
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''

  ## Patch pod can change image, test user can not do that
  # Command
  kube_flags=${kube_admin_flags[@]}
  ! kubectl patch "${kube_user_flags[@]}" pod valid-pod -p='{"spec":{"containers":[{"name": "kubernetes-serve-hostname", "image": "nginx"}]}}'
  kube::test::get_object_assert pods "{{range.items}}{{$image_field}}:{{end}}" 'gcr.io/google_containers/serve_hostname:'
  kubectl patch "${kube_admin_flags[@]}" pod valid-pod -p='{"spec":{"containers":[{"name": "kubernetes-serve-hostname", "image": "nginx"}]}}'
  # Post-condition: valid-pod POD has image nginx
  kube::test::get_object_assert pods "{{range.items}}{{$image_field}}:{{end}}" 'nginx:'

  ## Delete POD
  kubectl delete pod valid-pod "${kube_admin_flags[@]}" --grace-period=0

  ### Create two PODs from 1 yaml file
  # Pre-condition: no POD is running
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f docs/user-guide/multi-pod.yaml "${kube_admin_flags[@]}" --namespace=default
  # Post-condition: valid-pod and redis-proxy PODs are running
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'redis-master:redis-proxy:'

  ### Delete two PODs from 1 yaml file
  # Pre-condition: redis-master and redis-proxy PODs are running
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'redis-master:redis-proxy:'
  # Command
  kubectl delete -f docs/user-guide/multi-pod.yaml "${kube_admin_flags[@]}" --grace-period=0
  # Post-condition: no PODs are running
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''

  # Pre-Condition: no RC is running
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command: create the rc "nginx" with image nginx
  kubectl run nginx --image=nginx --save-config "${kube_admin_flags[@]}" --namespace=default
  # Post-Condition: rc "nginx" has configuration annotation
  ! kubectl get rc nginx --namespace=default -o yaml ${kube_user_flags[@]}
  [[ "$(kubectl get rc nginx --namespace=default -o yaml "${kube_admin_flags[@]}" | grep kubectl.kubernetes.io/last-applied-configuration)" ]]
  ## kubectl expose --save-config should generate configuration annotation
  # Pre-Condition: no service is running
  kube::test::get_object_assert svc "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:'
  # Command: expose the rc "nginx"
  kubectl expose rc nginx --save-config --port=80 --target-port=8000 "${kube_admin_flags[@]}"
  # Post-Condition: service "nginx" has configuration annotation
  ! kubectl get svc nginx --namespace=default -o yaml ${kube_user_flags[@]}
  [[ "$(kubectl get svc nginx --namespace=default -o yaml "${kube_admin_flags[@]}" | grep kubectl.kubernetes.io/last-applied-configuration)" ]]
  # Clean up
  ! kubectl delete rc,svc nginx ${kube_user_flags[@]}
  kubectl delete rc,svc nginx ${kube_admin_flags[@]}
  ## kubectl autoscale --save-config should generate configuration annotation
  # Pre-Condition: no RC is running, then create the rc "frontend", which shouldn't have configuration annotation
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" ''
  ! kubectl create -f examples/guestbook/frontend-controller.yaml "${kube_user_flags[@]}" --namespace=default
  kubectl create -f examples/guestbook/frontend-controller.yaml "${kube_user_flags[@]}" --namespace=test
  ! [[ "$(kubectl get rc frontend --namespace=test -o yaml "${kube_user_flags[@]}" | grep kubectl.kubernetes.io/last-applied-configuration)" ]]
  # Command: autoscale rc "frontend" 
  kubectl autoscale -f examples/guestbook/frontend-controller.yaml --save-config "${kube_user_flags[@]}" --max=2 --namespace=test
  # Post-Condition: hpa "frontend" has configuration annotation
  [[ "$(kubectl get hpa frontend --namespace=test -o yaml "${kube_user_flags[@]}" | grep kubectl.kubernetes.io/last-applied-configuration)" ]]
  # Clean up
  kubectl delete rc,hpa frontend ${kube_user_flags[@]} --namespace=test

  #################
  # Pod templates #
  #################

  ### Create PODTEMPLATE
  # Pre-condition: no PODTEMPLATE
  kube::test::get_object_assert podtemplates "{{range.items}}{{.metadata.name}}:{{end}}" ''
  # Command
  kubectl create -f docs/user-guide/walkthrough/podtemplate.json "${kube_admin_flags[@]}" --namespace=default
  # Post-condition: nginx PODTEMPLATE is available
  kube::test::get_object_assert podtemplates "{{range.items}}{{.metadata.name}}:{{end}}" 'nginx:'

  ! kubectl get podtemplates nginx "${kube_user_flags[@]}"
  ### Printing pod templates works
  kubectl get podtemplates "${kube_admin_flags[@]}"
  [[ "$(kubectl get podtemplates -o yaml "${kube_admin_flags[@]}" | grep nginx)" ]]

  ### Delete nginx pod template by name
  # Pre-condition: nginx pod template is available
  kube::test::get_object_assert podtemplates "{{range.items}}{{.metadata.name}}:{{end}}" 'nginx:'
  # Command
  ! kubectl delete podtemplate nginx "${kube_user_flags[@]}"
  kubectl delete podtemplate nginx "${kube_admin_flags[@]}"
  # Post-condition: No templates exist
  kube::test::get_object_assert podtemplate "{{range.items}}{{.metadata.name}}:{{end}}" ''

  ############
  # Services #
  ############

  kube::log::status "Testing kubectl(${version}:services)"

  kube_flags=${kube_admin_flags[@]}
  ### Create redis-master service from JSON
  # Pre-condition: Only the default kubernetes services are running
  kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:'
  # Command
  kubectl create -f examples/guestbook/redis-master-service.yaml "${kube_user_flags[@]}" --namespace=test
  # Post-condition: redis-master service is running
  kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:redis-master:'
  # Describe command should print detailed information
  kube::test::describe_object_assert services 'redis-master --namespace=test' "Name:" "Labels:" "Selector:" "IP:" "Port:" "Endpoints:" "Session Affinity:"
  # Describe command (resource only) should print detailed information
  kube::test::describe_resource_assert services "Name:" "Labels:" "Selector:" "IP:" "Port:" "Endpoints:" "Session Affinity:"

  kube_flags=${kube_user_flags[@]}
  kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'redis-master:'

  kubectl delete services redis-master --namespace=test --grace-period=0 ${kube_user_flags[@]}

  ###########################
  # Replication controllers #
  ###########################

  kube::log::status "Testing kubectl(${version}:replicationcontrollers)"

  kube_flags=${kube_admin_flags[@]}
  ### Create and stop controller, make sure it doesn't leak pods
  # Pre-condition: no replication controller is running
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f examples/guestbook/frontend-controller.yaml "${kube_admin_flags[@]}" --namespace=default
  ! kubectl get rc frontend "${kube_user_flags[@]}" --namespace=default
  ! kubectl delete rc frontend "${kube_user_flags[@]}" --grace-period=0 --namespace=default
  kubectl delete rc frontend "${kube_admin_flags[@]}" --grace-period=0 --namespace=default
  # Post-condition: no pods from frontend controller
  kube::test::get_object_assert 'pods -l "name=frontend"' "{{range.items}}{{$id_field}}:{{end}}" ''

  kube_flags=${kube_user_flags[@]}
  ### Create replication controller frontend from JSON
  # Pre-condition: no replication controller is running
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f examples/guestbook/frontend-controller.yaml "${kube_user_flags[@]}" --namespace=test
  # Post-condition: frontend replication controller is running
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" 'frontend:'
  # Describe command should print detailed information
  kube::test::describe_object_assert rc 'frontend --namespace=test' "Name:" "Image(s):" "Labels:" "Selector:" "Replicas:" "Pods Status:"
  # Describe command (resource only) should print detailed information
  kube::test::describe_resource_assert rc "Name:" "Name:" "Image(s):" "Labels:" "Selector:" "Replicas:" "Pods Status:"

  ### Scale replication controller frontend with current-replicas and replicas
  # Pre-condition: 3 replicas
  kube::test::get_object_assert 'rc frontend --namespace=test' "{{$rc_replicas_field}}" '3'
  # Command
  kubectl scale --current-replicas=3 --replicas=2 replicationcontrollers frontend "${kube_user_flags[@]}" --namespace=test
  # Post-condition: 2 replicas
  kube::test::get_object_assert 'rc frontend --namespace=test' "{{$rc_replicas_field}}" '2'

  ######################
  # Persistent Volumes #
  ######################

  kube_flags=${kube_admin_flags[@]}
  ### Create and delete persistent volume examples
  # Pre-condition: no persistent volumes currently exist
  kube::test::get_object_assert pv "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f docs/user-guide/persistent-volumes/volumes/local-01.yaml "${kube_admin_flags[@]}" --namespace=default
  kube::test::get_object_assert pv "{{range.items}}{{$id_field}}:{{end}}" 'pv0001:'
  kubectl delete pv pv0001 "${kube_admin_flags[@]}" --namespace=default
  kubectl create -f docs/user-guide/persistent-volumes/volumes/local-02.yaml "${kube_admin_flags[@]}" --namespace=default
  kube::test::get_object_assert pv "{{range.items}}{{$id_field}}:{{end}}" 'pv0002:'
  kubectl delete pv pv0002 "${kube_admin_flags[@]}" --namespace=default
  kubectl create -f docs/user-guide/persistent-volumes/volumes/gce.yaml "${kube_admin_flags[@]}" --namespace=default
  kube::test::get_object_assert pv "{{range.items}}{{$id_field}}:{{end}}" 'pv0003:'
  kubectl delete pv pv0003 "${kube_admin_flags[@]}" --namespace=default
  # Post-condition: no PVs
  kube::test::get_object_assert pv "{{range.items}}{{$id_field}}:{{end}}" ''

  ############################
  # Persistent Volume Claims #
  ############################

  kube_flags=${kube_admin_flags[@]}
  ### Create and delete persistent volume claim examples
  # Pre-condition: no persistent volume claims currently exist
  kube::test::get_object_assert pvc "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f docs/user-guide/persistent-volumes/claims/claim-01.yaml "${kube_admin_flags[@]}" --namespace=default
  kube::test::get_object_assert pvc "{{range.items}}{{$id_field}}:{{end}}" 'myclaim-1:'
  kubectl delete pvc myclaim-1 "${kube_admin_flags[@]}" --namespace=default

  kubectl create -f docs/user-guide/persistent-volumes/claims/claim-02.yaml "${kube_admin_flags[@]}" --namespace=default
  kube::test::get_object_assert pvc "{{range.items}}{{$id_field}}:{{end}}" 'myclaim-2:'
  kubectl delete pvc myclaim-2 "${kube_admin_flags[@]}" --namespace=default

  kubectl create -f docs/user-guide/persistent-volumes/claims/claim-03.json "${kube_admin_flags[@]}" --namespace=default
  kube::test::get_object_assert pvc "{{range.items}}{{$id_field}}:{{end}}" 'myclaim-3:'
  kubectl delete pvc myclaim-3 "${kube_admin_flags[@]}" --namespace=default
  # Post-condition: no PVCs
  kube::test::get_object_assert pvc "{{range.items}}{{$id_field}}:{{end}}" ''


  #########
  # Nodes #
  #########

  kube::log::status "Testing kubectl(${version}:nodes)"

  kube_flags=${kube_admin_flags[@]}
  kube::test::get_object_assert nodes "{{range.items}}{{$id_field}}:{{end}}" '127.0.0.1:'

  kube::test::describe_object_assert nodes "127.0.0.1 --namespace=default" "Name:" "Labels:" "CreationTimestamp:" "Conditions:" "Addresses:" "Capacity:" "Pods:"
  # Describe command (resource only) should print detailed information
  kube::test::describe_resource_assert nodes "Name:" "Labels:" "CreationTimestamp:" "Conditions:" "Addresses:" "Capacity:" "Pods:"

  ### kubectl patch update can mark node unschedulable
  # Pre-condition: node is schedulable
  kube::test::get_object_assert "nodes 127.0.0.1 --namespace=default" "{{.spec.unschedulable}}" '<no value>'
  kubectl patch "${kube_admin_flags[@]}" nodes "127.0.0.1" --namespace=default -p='{"spec":{"unschedulable":true}}'
  # Post-condition: node is unschedulable
  kube::test::get_object_assert "nodes 127.0.0.1 --namespace=default" "{{.spec.unschedulable}}" 'true'
  kubectl patch "${kube_admin_flags[@]}" nodes "127.0.0.1" --namespace=default -p='{"spec":{"unschedulable":null}}'
  # Post-condition: node is schedulable
  kube::test::get_object_assert "nodes 127.0.0.1 --namespace=default" "{{.spec.unschedulable}}" '<no value>'

  # Common user can not access the node resource
  ! kubectl get nodes ${kube_user_flags[@]}
  ! kubectl patch "${kube_user_flags[@]}" nodes "127.0.0.1" -p='{"spec":{"unschedulable":true}}'
  ! kubectl patch "${kube_user_flags[@]}" nodes "127.0.0.1" -p='{"spec":{"unschedulable":null}}'

  ###########
  # Swagger #
  ###########

  if [[ -n "${version}" ]]; then
    # Verify schema
    file="${KUBE_TEMP}/schema-${version}.json"
    curl -s "http://127.0.0.1:${API_PORT}/swaggerapi/api/${version}" > "${file}"
    [[ "$(grep "list of returned" "${file}")" ]]
    [[ "$(grep "List of pods" "${file}")" ]]
    [[ "$(grep "Watch for changes to the described resources" "${file}")" ]]
  fi

  kubectl delete "${kube_admin_flags[@]}" rc,pods --all --grace-period=0
}

kube_api_versions=(
  v1
)
for version in "${kube_api_versions[@]}"; do
  KUBE_API_VERSIONS="v1,extensions/v1beta1" runTests "${version}"
done

kube::log::status "TEST PASSED"
