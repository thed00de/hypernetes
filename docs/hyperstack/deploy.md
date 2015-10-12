<!-- BEGIN MUNGE: UNVERSIONED_WARNING -->

<!-- BEGIN STRIP_FOR_RELEASE -->

<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">
<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">
<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">
<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">
<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">

<h2>PLEASE NOTE: This document applies to the HEAD of the source tree</h2>

If you are using a released version of Kubernetes, you should
refer to the docs that go with that version.

<strong>
The latest 1.0.x release of this document can be found
[here](http://releases.k8s.io/release-1.0/docs/hyperstack/deploy.md).

Documentation for other releases can be found at
[releases.k8s.io](http://releases.k8s.io).
</strong>
--

<!-- END STRIP_FOR_RELEASE -->

<!-- END MUNGE: UNVERSIONED_WARNING -->

# HyperStack - deployment

This document covers the basic depolyment procedure for HyperStack. If you are looking for detailed depolyment of kubernetes, refer to [http://kubernetes.io/gettingstarted/](http://kubernetes.io/gettingstarted/).

## Deploy OpenStack

There are a mass of OpenStack deployment solutions, including

* [OpenStack Installation Guide for Ubuntu 14.04](http://docs.openstack.org/kilo/install-guide/install/apt/content/)
* [OpenStack Installation Guide for Red Hat](http://docs.openstack.org/kilo/install-guide/install/yum/content/)
* [RDO](https://www.rdoproject.org/Main_Page)
* [MAAS](http://www.ubuntu.com/download/cloud/install-ubuntu-openstack)
* [Fuel](https://www.mirantis.com/products/mirantis-openstack-software/)
* [devstack](http://docs.openstack.org/developer/devstack/)
* [Puppet](https://github.com/puppetlabs/puppetlabs-openstack)
* And so on

Choose any tool you like to deploy a new OpenStack, or you can just use existing  OpenStack environment.

## Deploy Kubernetes

Since HyperStack is kubernetes based, you can deploy HyperStack by same procedure as kubernetes. Here is some distro links:

* [deploy kubernetes on centos](../../docs/getting-started-guides/centos/centos_manual_config.md)
* [deploy kubernetes on ubuntu](../../docs/getting-started-guides/ubuntu.md)
* [deploy kubernetes on fedora](../../docs/getting-started-guides/fedora/fedora_manual_config.md)

See more at [kuberntes getstarted guide](../../docs/getting-started-guides/)

## Deploy kubestack

KubeStack is an OpenStack network provider for kubernetes.

```shell
cd $GOPATH
git clone https://github.com/hyperhq/kubestack.git
cd kubestack
make && make install
```

Config kubestack

```shell
# cat /etc/kubestack.conf
[Global]
auth-url = http://172.31.17.48:5000/v2.0
username = admin
password = admin
tenant-name = admin
region = RegionOne
ext-net-id = c0a96e14-b90f-49ef-b1d7-86321d55f7a0

[LoadBalancer]
create-monitor = yes
monitor-delay = 1m
monitor-timeout = 30s
monitor-max-retries = 3

[Plugin]
plugin-name = ovs
```

```shell
# cat /usr/lib/systemd/system/kubestack.service
[Unit]
Description=OpenStack Network Provider for Kubernetes
After=syslog.target network.target openvswitch.service
Requires=openvswitch.service

[Service]
ExecStart=/usr/local/bin/kubestack \
  -logtostderr=false -v=4 \
  -group=kube \
  -log_dir=/var/log/kubestack \
  -conf=/etc/kubestack.conf
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

## Configure Kubernetes

Disable selinux

```shell
sed -i 's/SELINUX=enforcing/SELINUX=disabled/g' /etc/selinux/config
setenforce 0
```

Create service account key

```shell
mkdir /var/lib/kubernetes/
openssl genrsa -out /var/lib/kubernetes/serviceaccount.key 2048
chown kube:kube /var/lib/kubernetes/serviceaccount.key
```

Create kubernetes log dir

```shell
mkdir /var/log/kubernetes
chown kube:kube kubernetes/
```

Config etcd

```shell
cat >> /etc/etcd/etcd.conf <<EOF
ETCD_LISTEN_CLIENT_URLS="http://master:2379"
EOF
```

Common configs for all kubernetes services

```shell
# cat /etc/kubernetes/config
# logging to stderr means we get it in the systemd journal
KUBE_LOGTOSTDERR="--logtostderr=false --log-dir=/var/log/kubernetes"
# journal message level, 0 is debug
KUBE_LOG_LEVEL="--v=4”
# Should this cluster be allowed to run privileged docker containers
KUBE_ALLOW_PRIV="--allow_privileged=false"
# How the controller-manager, scheduler, and proxy find the apiserver
KUBE_MASTER="--master=http://master:8080"
```

Config kube-apiserver

```
# cat /etc/kubernetes/apiserver
# The address on the local server to listen to.
KUBE_API_ADDRESS="--insecure-bind-address=0.0.0.0"
# The port on the local server to listen on.
# KUBE_API_PORT="--port=8080"
# Port minions listen on
# KUBELET_PORT="--kubelet_port=10250"
# Comma separated list of nodes in the etcd cluster
KUBE_ETCD_SERVERS="--etcd_servers=http://master:2379"
# Address range to use for services
KUBE_SERVICE_ADDRESSES="--service-cluster-ip-range=10.254.0.0/16"
# default admission control policies
KUBE_ADMISSION_CONTROL="--admission_control=NamespaceLifecycle,NamespaceExists,LimitRanger,SecurityContextDeny,ServiceAccount,ResourceQuota"
# Add your own!
KUBE_API_ARGS="--service-account-key-file=/var/lib/kubernetes/serviceaccount.key --experimental-keystone-url=https://localhost:2349"
```

Config kube-controller-manager

```shell
# cat /etc/kubernetes/controller-manager
### 
# The following values are used to configure the kubernetes controller-manager
# defaults from config and apiserver should be adequate

# Add your own!
KUBE_CONTROLLER_MANAGER_ARGS="--service-account-private-key-file=/var/lib/kubernetes/serviceaccount.key --network-provider=openstack"
```

Config kube-proxy

```shell
# cat /etc/kubernetes/proxy
### 
# kubernetes proxy config
# default config should be adequate

# Add your own!
KUBE_PROXY_ARGS="--proxy-mode=haproxy"
```

Config kubelet

```shell
# cat /etc/kubernetes/kubelet
# The address for the info server to serve on (set to 0.0.0.0 or "" for all interfaces)
KUBELET_ADDRESS="--address=0.0.0.0"
# The port for the info server to serve on
# KUBELET_PORT="--port=10250"
# You may leave this blank to use the actual hostname
KUBELET_HOSTNAME="--hostname_override=ostack"
# location of the api-server
KUBELET_API_SERVER="--api_servers=http://ostack:8080"
# Add your own!
KUBELET_ARGS="--container-runtime=hyper --network-provider=openstack --cinder-config=/etc/kubernetes/cinder.conf"

# cat /etc/kubernetes/cinder.conf
[Global]
auth-url = http://172.31.17.48:5000/v2.0
username = admin
password = admin
tenant-name = admin
region = RegionOne

[RBD]
keyring = "AQAtqv9V3u4nKRAA9xfic687DqPW1FV/rly3nw=="
```

## Start services

```shell
systemctl restart etcd
systemctl restart hyperd
systemctl restart kubestack
systemctl restart kube-apiserver.service
systemctl restart kube-scheduler.service
systemctl restart kube-controller-manager.service
systemctl restart kubelet.service
systemctl restart kube-proxy
````

<!-- BEGIN MUNGE: GENERATED_ANALYTICS -->
[![Analytics](https://kubernetes-site.appspot.com/UA-36037335-10/GitHub/docs/hyperstack/deploy.md?pixel)]()
<!-- END MUNGE: GENERATED_ANALYTICS -->
