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

  # https://github.com/hyperhq/hypernetes/issues/52
  # After the issue is fixed, the following test case shoule be removed
  kubectl delete tenant test "${kube_admin_flags[@]}" --grace-period=0
  kube::test::get_object_assert "tenants test" "{{.status.phase}}" 'Terminating'

  #################################
  # namespace creation / deletion #
  #################################
  kube_flags=${kube_admin_flags[@]}
  kube::test::get_object_assert "namespaces" "{{range.items}}{{$id_field}}:{{end}}" 'default:'
  kube_flags=${kube_user_flags[@]}
  kube::test::get_object_assert "namespaces" "{{range.items}}{{$id_field}}:{{end}}" ''

  # Create 'test' tenant
  # kubectl create -f docs/user-guide/te.yaml "${kube_admin_flags[@]}"
  # Create 'test' namespace for 'test' tenant
  # kubectl create -f docs/user-guide/ns.yaml "${kube_user_flags[@]}"
  # kube::test::get_object_assert "namespaces" "{{range.items}}{{$id_field}}:{{end}}" 'test:'

  ###########################
  # POD creation / deletion #
  ###########################

  kube::log::status "Testing kubectl(${version}:pods)"

  kube_flags=${kube_admin_flags[@]}
  ### Create POD valid-pod from JSON
  # Pre-condition: no POD is running
  kube::test::get_object_assert "pods" "{{range.items}}{{$id_field}}:{{end}}" ''


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
