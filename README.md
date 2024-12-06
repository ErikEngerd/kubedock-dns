
# Motivation

This project is prototype for showing how it is possible to simulate a docker network
concept in a single kubernetes namespace.

In docker, containers are attached to a network and containers attached to the same
network can contact each other using their hostname. It is possible to run different
containers with the same host names in different networks and these can run side by
side without interference.

For instance, it is possible to have two containers with hostname 'db' and 'service' running
in one docker network and two cther containers with the same hostnames running in another.

When using testcontainers with kubedock, kubedock creates containers with hostnames
but uses a flat network approach. This means that hostnames are represented by services
and containers communicate with each other using services. This limits the
possibility of running concurrent tests that use the same hostnames for containers.
In kubedock this is currently solved by 'locking' the namespaces so that tests cannot
be run concurrently.

This project is a prototype showing how to fix this problem, allowing multiple
concurrent tests to be run in the same namespace without conflict and without the
need for locking the namespace.

# Conceptual solution

In each pod used by kubedock to simulate a docker 'run', annotations are defined to
define the hostname and network that the pod belongs to. For instance

```
apiVersion: v1
kind: Pod
metadata:
  annotations:
    kubedock.hostalias/0: "db"
    kubedock.network/0: "test1"
  labels:
    kubedock: "true"
  name: db1
  namespace: kubedock
spec:
  containers:
     ....
```

The above pod has one hostname 'db' and belongs to the network named 'test1'. When running multiple
pods annotated this way, one gets a view of the network. E.g. there can be a network 'test1' with hosts
'db' and 'service', and another network 'test2' with hosts 'db' and 'service'. The second part in this
setup is that DNS lookups of 'db' from 'service' in network 'test1' should resolve to the 'db' pod in
network 'test1', and similarly the lookups of 'db' by 'service' in network 'test2' should resolve to
the 'db' pod in network 'test2'. See for instance the examples in the test directory.

This behavior can be achieved as follows:
* by watching deployments in the namespace it is possible to build a a datastructure as follows
  * a mapping of pod IP to network
  * a mapping of hostnames to pod IP in the network and vice versa
* this datastructure is resolved by a DNS server that looks at the source IP of the DNS
  lookup, which identifies the network. Then within the network, the IP can be looked up based on
  hostname (A record), of viceversa, the hostname looked up by IP (PTR record). This DNS server
  is colocated with the above watcher in the same component. When a record cannot be resolved,
  the DNS server delegates to the upstream DNS server which is the standard kubernetes DNS server.
* on deployment of pods, a pod is mutated using the dnsPolicy and dnsConfig fields to use the
  DNS server for lookups. The mutator is limited to mutating only pods with a certain label
  ("kubedock" currently), so-called 'opt-in' so that we have control on which pods are
  mutated.

# Technical details

* An embedded DNS server is used based on miekg/dns, the same DNS package used by coredns, the standard
  nameserver of kubernetes. This allows us to use the source IP for DNS lookups.
* The client-go Informer is used to keep up to date with changes to pods
* The controller-runtime package is used for implementing the mutating admission controller.

kubedock-dns tries to validate the network setup on deployment of pods. It also prevents changes to the
network setup after deployment, but still allows any other changes. The idea is to achieve fast failure
for detecting problems.

# Kubedock changes

The advantage of this model is that it is relatively ueasy to integrate with kubedock. All that needs
to be done is instrument kubedock to add the required annotations and label (and/or modify or configure
kubedock-dns) so that the mechanism can be used. This solution came out of discussions with the
kubedock maintainer. The next step is to prototype the required changes in kubedock to make it work.

# Usage

Currently this project does not have any deliveries but those will come soon in the form of docker 
images and a helm chart. 

For testing locally, the steps are described below. 

## Prerequisites

A local docker runtime, and a docker registry running at localhost port 5000.
A kubernetes cluster (e.g. k3d) that can pull from the localhost port 5000. 

Using k3d a cluster is created as follows: 
```
k3d cluster create dev --registry-config registries.yaml 
```
with the content of `registries.yaml` as follows: 
```
mirrors:
  localhost:5000:
    endpoint:
      - http://host.k3d.internal:5000
```

The above setup makes sure the k3d can pull images from localhost:5000. 

## Building

Run the following to build and push the images. 
```
make push
```

## Installing kubedock-dns

```
helm upgrade --install kubedock-dns helm/dns 
```

## Try out the examples

Try some of the examples in the test directory and look at the 
logging of the the dns-server hat was installed by the helm chart. 

## Run a local test with kubedock-dns

In one terminal:
```
kubedock server --port-forward --disable-services
```
The --disable-services flag is required to be able to run
multiple concurrent jobs. 

In another terminal in your own project
```
export TESTCONTAINERS_RYUK_DISABLED=true  ## optional, can be enabled
export TESTCONTAINERS_CHECKS_DISABLE=true ## optional, can be enabled
export DOCKER_HOST=tcp://127.0.0.1:2475
```

Next, run your integration tests using testcontainers. 






