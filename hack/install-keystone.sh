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

set -o errexit
set -o nounset
set -o pipefail
set -v

sudo apt-get update -qq
sudo apt-get install -y python python-pip
git clone https://git.openstack.org/openstack/keystone.git /tmp/keystone
cd /tmp/keystone
sudo pip install -r requirements.txt
sudo python setup.py install
sudo mkdir /etc/keystone/

cd -
sudo cp ./hack/testdata/keystone.conf /etc/keystone/keystone.conf
sudo cp /tmp/keystone/etc/* /etc/keystone/
sudo cp /etc/keystone/logging.conf.sample /etc/keystone/logging.conf

/bin/sh -c "keystone-manage db_sync" keystone

keystone-all &
KEYSTONE_PID=$!

# sleep 5s to wait for 'keystone-all'
sleep 5
sudo chmod 777 /tmp/keystone.db

export OS_SERVICE_TOKEN="781568961179329e52c4"
export OS_SERVICE_ENDPOINT="http://127.0.0.1:35357/v2.0"
export OS_AUTH_URL="http://127.0.0.1:35357/v2.0"

keystone tenant-create --name=default --description="Admin Tenant"
keystone user-create --name=admin --pass=admin
keystone role-create --name=admin
keystone user-role-add --user=admin --tenant=default --role=admin

keystone tenant-create --name=test --description="Test Tenant"
keystone user-create --name=test --pass=test
keystone role-create --name=test
keystone user-role-add --user=test --tenant=test --role=test

keystone service-create --name=keystone --type=identity --description="Keystone Identity Service"

cat > create_endpoint.py <<_EOF_
#!/usr/bin/env python

from keystoneclient.v2_0 import client

token = '781568961179329e52c4'
endpoint = 'http://127.0.0.1:35357/v2.0'
keystone = client.Client(token=token, endpoint=endpoint)
services = keystone.services.list()
my_service = [x for x in services if x.name=='keystone'][0]
keystone.endpoints.create(region="RegionOne", service_id=my_service.id, publicurl="http://127.0.0.1:5000/v2.0", adminurl="http://127.0.0.1:35357/v2.0", internalurl="http://127.0.0.1:5000/v2.0")
_EOF_

python create_endpoint.py

rm -f create_endpoint.py

[[ -n "${KEYSTONE_PID-}" ]] && kill "${KEYSTONE_PID}" 1>&2 2>/dev/null
sleep 5
