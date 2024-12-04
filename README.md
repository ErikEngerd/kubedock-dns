
# Motivation

This project is prototype for showing how it is possible to simulate a docker network
concept in a single kubernetes namespace.

In docker, containers are attached to a network and containers attached to the same
network can contact each other using their hostname. It is possible to run different
container with the same host names in different networks and these can run side by
die without interference.

For instance, it is possible to have two containers with hostname 'db' and 'service' running
in one docker network and two cther containers with the same hostnames running in another.

When using testcontainers with kubedock, kubedock creates containers with hostnames
but uses a flat network approach. This means that hostnames are represented by services
and containers communicate with each other using servicdes. This however limits the
possibility of running concurrent tests that use the same hostnames for containers.
In kubedock this is currently solved by 'locking' the namespaces so that tests cannot
be run concurrently.

This project is a prototype showing how to fix this problem, allowing multiple
concurrent tests to be run in the same namespace without conflict and without the
need for locking the namespace.

# Conceptual solution

Each pod used by kubedock to simulate a docker 'run', annotations are defined to
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

kubedock-dns tries to validate the network setup on deployment of pods. It also pervents changes to the
network setup after deployment, but still allows any other changes. The idea is to achieve fast failure
for detecting problems.

# Kubedock changes

The advantage of this model is that it is relatively ueasy to integrate with kubedock. All that needs
to be done is instrument kubedock to add the required annotations and label (and/or modify or configure
kubedock-dns) so that the mechanism can be used. This solution came out of discussions with the
kubedock maintainer. The next step is to prototype the required changes in kubedock to make it work.







