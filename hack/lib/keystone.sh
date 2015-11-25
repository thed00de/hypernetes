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

# A set of helpers for starting/running keystone for tests

kube::keystone::start() {
  local host=${KEYSTONE_HOST:-127.0.0.1}
  local port=${KEYSTONE_PORT:-5000}

  which keystone-all >/dev/null || {
    kube::log::usage "keystone-all must be in your PATH"
    exit 1
  }

  if pgrep keystone-all >/dev/null 2>&1; then
    kube::log::usage "keystone appears to already be running on this machine (`pgrep -l keystone`) (or its a zombie and you need to kill its parent)."
    kube::log::usage "retry after you resolve this keystone error."
    exit 1
  fi

  # Start keystone
  kube::log::info "keystone-all >/dev/null 2>/dev/null"
  keystone-all --logfile /tmp/keystone.log 2>/dev/null &
  KEYSTONE_PID=$!
  sleep 5

  echo "Waiting for keystone to come up."
  kube::util::wait_for_url "http://${host}:${port}/v2.0" "keystone: " 0.25 80
}

kube::keystone::stop() {
  kill "${KEYSTONE_PID-}" >/dev/null 2>&1 || :
  wait "${KEYSTONE_PID-}" >/dev/null 2>&1 || :
}

kube::keystone::clean_keystone_dir() {
  rm -rf "${KEYSTONE_DIR-}"
}

kube::keystone::cleanup() {
  kube::keystone::stop
  kube::keystone::clean_keystone_dir
}
