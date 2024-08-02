---
title: "Exchanging messages over an ssl ingress"  
description: "Steps to get a producer and a consummer exchanging messages over a deployed broker on kubernetes using an ingress"
draft: false
images: []
menu:
  docs:
    parent: "send-receive"
weight: 110
toc: true
---

### Prerequisite

Before you start, you need to have access to a running Kubernetes cluster
environment. A [Minikube](https://minikube.sigs.k8s.io/docs/start/) instance
running on your laptop will do fine.

#### Start minikube with a parametrized dns domain name

```{"stage":"init", "id":"minikube_start"}
$ minikube start

* minikube v1.32.0 on Fedora 39
* Automatically selected the docker driver. Other choices: kvm2, qemu2, none, ssh
* Using Docker driver with root privileges
* Starting control plane node minikube in cluster minikube
* Pulling base image ...
* Creating docker container (CPUs=2, Memory=15900MB) ...
* Preparing Kubernetes v1.28.3 on Docker 24.0.7 ...
  - Generating certificates and keys ...
  - Booting up control plane ...
  - Configuring RBAC rules ...
* Configuring bridge CNI (Container Networking Interface) ...
  - Using image gcr.io/k8s-minikube/storage-provisioner:v5
* Verifying Kubernetes components...
* Enabled addons: storage-provisioner, default-storageclass
* Done! kubectl is now configured to use "minikube" cluster and "default" namespace by default
```

#### Enable nginx and ssl passthrough for minikube

```{"stage":"init"}
$ minikube addons enable ingress

* ingress is an addon maintained by Kubernetes. For any concerns contact minikube on GitHub.
You can view the list of minikube maintainers at: https://github.com/kubernetes/minikube/blob/master/OWNERS
  - Using image registry.k8s.io/ingress-nginx/kube-webhook-certgen:v20231011-8b53cabe0
  - Using image registry.k8s.io/ingress-nginx/kube-webhook-certgen:v20231011-8b53cabe0
  - Using image registry.k8s.io/ingress-nginx/controller:v1.9.4
* Verifying ingress addon...
* The 'ingress' addon is enabled

$ minikube kubectl -- patch deployment -n ingress-nginx ingress-nginx-controller --type='json' -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value":"--enable-ssl-passthrough"}]'

deployment.apps/ingress-nginx-controller patched
```

#### Get minikube's ip

```{"stage":"init", "variables":["CLUSTER_IP"], "runtime":"bash", "label":"get the cluster ip"}
$ CLUSTER_IP=$(minikube ip)
```

#### Make sure the domain of your cluster is resolvable

If you are running your OpenShift cluster locally, you might not be able to
resolve the urls to IPs out of the blue. Follow [this guide]({{< ref
"/docs/help/hostname_resolution" >}} "set up dnsmasq") to configure your setup.

This tutorial will follow the simple /etc/hosts approach, but feel free to use
the most appropriate one for you.

### Deploy the operator

#### create the namespace

```{"stage":"init"}
$ kubectl create namespace send-receive-project

namespace/send-receive-project created

$ kubectl config set-context --current --namespace=send-receive-project

Context "minikube" modified.
```

Go to the root of the operator repo and install it:

```{"stage":"init", "rootdir":"$operator"}
$ ./deploy/install_opr.sh

Deploying operator to watch single namespace
Client Version: 4.15.0-0.okd-2024-01-27-070424
Kustomize Version: v5.0.4-0.20230601165947-6ce0bf390ce3
Kubernetes Version: v1.28.3
customresourcedefinition.apiextensions.k8s.io/activemqartemises.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/activemqartemisaddresses.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/activemqartemisscaledowns.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/activemqartemissecurities.broker.amq.io created
serviceaccount/activemq-artemis-controller-manager created
role.rbac.authorization.k8s.io/activemq-artemis-operator-role created
rolebinding.rbac.authorization.k8s.io/activemq-artemis-operator-rolebinding created
role.rbac.authorization.k8s.io/activemq-artemis-leader-election-role created
rolebinding.rbac.authorization.k8s.io/activemq-artemis-leader-election-rolebinding created
deployment.apps/activemq-artemis-controller-manager created
```

Wait for the Operator to start (status: `running`).

```{"stage":"init", "runtime":"bash", "label":"wait for the operator to be running"}
$ kubectl wait pod --for=condition=Ready --namespace=send-receive-project $(kubectl get pods --namespace send-receive-project | awk '/activemq-artemis-controller-manager/ {print $1;exit}')

pod/activemq-artemis-controller-manager-69996958dc-rnclt condition met
```
### Deploy the ActiveMQ Artemis Broker

For this tutorial we need to:

* have a broker that is able to listen to any network interface. For that we
  setup an `acceptor` that will be listening on every interfaces on port
  `62626`.
* have the ssl protocol configured for the `acceptor`
* have queues to exchange messages on. These are configured by the broker
  properties. Two queues are setup, one called `APP.JOBS` that is of type
  `ANYCAST` and one called `APP.COMMANDS` that is of type `MULTICAST`.

#### Create the certs

We'll take some inspiration from the [ssl broker
setup](https://github.com/artemiscloud/send-receive-project/blob/main/docs/tutorials/ssl_broker_setup.md)
to configure the certificates.

> [!NOTE]
> In this tutorial:
> * The password used for the certificates is `000000`.
> * The secret name is `send-receive-sslacceptor-secret` composed from the broker
>   name `send-receive` and the acceptor name `sselacceptor`

```{"stage":"cert-creation", "rootdir":"$tmpdir.1", "runtime":"bash", "label":"generate cert"}
$ printf '000000\n000000\n\n\n\n\n\n\nyes\n' | keytool -genkeypair -alias artemis -keyalg RSA -keysize 2048 -storetype PKCS12 -keystore broker.ks -validity 3000
$ printf '000000\n' | keytool -export -alias artemis -file broker.cert -keystore broker.ks
$ printf '000000\n000000\nyes\n' | keytool -import -v -trustcacerts -alias artemis -file broker.cert -keystore client.ts

Owner: CN=Unknown, OU=Unknown, O=Unknown, L=Unknown, ST=Unknown, C=Unknown
Issuer: CN=Unknown, OU=Unknown, O=Unknown, L=Unknown, ST=Unknown, C=Unknown
Serial number: 37372d20
Valid from: Thu Aug 01 17:55:46 CEST 2024 until: Mon Oct 18 17:55:46 CEST 2032
Certificate fingerprints:
	 SHA1: 02:48:8B:E2:3A:6A:3D:64:F0:C8:F3:FA:61:FA:B3:55:59:AA:90:DE
	 SHA256: E7:3F:0F:65:1F:E9:1F:39:F0:D3:0C:B5:0C:71:7A:E3:12:90:2A:AD:9B:77:26:3D:E8:0C:C8:DA:88:FE:CD:F2
Signature algorithm name: SHA256withRSA
Subject Public Key Algorithm: 2048-bit RSA key
Version: 3

Extensions: 

#1: ObjectId: 2.5.29.14 Criticality=false
SubjectKeyIdentifier [
KeyIdentifier [
0000: 15 45 6D 5C C1 0B AA A1   9F A0 DB 97 BD 82 5A 39  .Em\..........Z9
0010: 66 04 D1 8E                                        f...
]
]

```

Create the secret in kubernetes

```{"stage":"cert-creation", "rootdir":"$tmpdir.1"}
$ kubectl create secret generic send-receive-sslacceptor-secret --from-file=broker.ks --from-file=client.ts --from-literal=keyStorePassword='000000' --from-literal=trustStorePassword='000000' -n send-receive-project

secret/send-receive-sslacceptor-secret created
```

Get the path of the cert folder for later

```{"stage":"cert-creation", "rootdir":"$tmpdir.1", "runtime":"bash", "label":"get cert folder", "variables":["CERT_FOLDER"]}
$ CERT_FOLDER=$(pwd)
```

#### Start the broker

```{"stage":"deploy", "HereTag":"EOF", "runtime":"bash", "label":"deploy the broker", "env":["CLUSTER_IP"], "breakpoint":true}
$ kubectl apply -f - << EOF
apiVersion: broker.amq.io/v1beta1
kind: ActiveMQArtemis
metadata:
  name: send-receive
  namespace: send-receive-project
spec:
  ingressDomain: 192.168.49.2.nip.io
  acceptors:
    - name: sslacceptor
      port: 62626
      expose: true
      sslEnabled: true
      sslSecret: send-receive-sslacceptor-secret
  brokerProperties:
    - addressConfigurations."APP.JOBS".routingTypes=ANYCAST
    - addressConfigurations."APP.JOBS".queueConfigs."APP.JOBS".routingType=ANYCAST
    - addressConfigurations."APP.COMMANDS".routingTypes=MULTICAST
EOF
```

Wait for the Broker to be ready:

```{"stage":"deploy"}
$ kubectl wait ActiveMQArtemis send-receive --for=condition=Ready --namespace=send-receive-project --timeout=240s

activemqartemis.broker.amq.io/send-receive condition met
```

#### Create a route to access the ingress:

Check for the ingress availability:

```
$ kubectl get ingress --show-labels
NAME                                 CLASS   HOSTS                                                                     ADDRESS         PORTS     AGE   LABELS
send-receive-sslacceptor-0-svc-ing   nginx   ing.sslacceptor.send-receive-0.send-receive-project.demo.artemiscloud.io  192.168.39.68   80, 443   18s   ActiveMQArtemis=send-receive,application=send-receive-app,statefulset.kubernetes.io/pod-name=send-receive-ss-0
```

### Exchanging messages between a producer and a consumer

Download the [latest
release](https://activemq.apache.org/components/artemis/download/) of ActiveMQ
Artemis, decompress the tarball and locate the artemis executable.

```{"stage":"test_setup", "rootdir":"$tmpdir.1", "runtime":"bash", "label":"download artemis"}
$ wget --quiet https://dlcdn.apache.org/activemq/activemq-artemis/2.36.0/apache-artemis-2.36.0-bin.tar.gz
$ tar -zxf apache-artemis-2.36.0-bin.tar.gz apache-artemis-2.36.0/
```

The `artemis` will need to point to the https endopint generated in earlier with
a couple of parameters set:
* `sslEnabled` = `true`
* `verifyHost` = `false`
* `trustStorePath` = `/some/path/broker.ks`
* `trustStorePassword` = `000000`

To use the consumer and the producer you'll need to give the path to the
`broker.ks` file you've created earlier. In the following commands the file is
located to `${CERT_FOLDER}/broker.ks`.

#### ANYCAST

For this use case, run first the producer, then the consumer.

```{"stage":"test1", "rootdir":"$tmpdir.1/apache-artemis-2.36.0/bin/", "runtime":"bash", "env":["INGRESS_URL", "CERT_FOLDER"], "label":"anycast: produce 1000 messages"}
$ ./artemis producer --destination queue://APP.JOBS --url "tcp://${INGRESS_URL}:443?sslEnabled=true&verifyHost=false&trustStorePath=${CERT_FOLDER}/broker.ks&trustStorePassword=000000"

Connection brokerURL = tcp://send-receive-sslacceptor-0-svc-ing-send-receive-project.demo.artemiscloud.io:443?sslEnabled=true&verifyHost=false&trustStorePath=/tmp/2876485925/broker.ks&trustStorePassword=000000
Producer ActiveMQQueue[APP.JOBS], thread=0 Started to calculate elapsed time ...

Producer ActiveMQQueue[APP.JOBS], thread=0 Produced: 1000 messages
Producer ActiveMQQueue[APP.JOBS], thread=0 Elapsed time in second : 8 s
Producer ActiveMQQueue[APP.JOBS], thread=0 Elapsed time in milli second : 8460 milli seconds
```

```{"stage":"test1", "rootdir":"$tmpdir.1/apache-artemis-2.36.0/bin/", "runtime":"bash", "env":["INGRESS_URL", "CERT_FOLDER"], "label":"anycast: consume 1000 messages"}
$ ./artemis consumer --destination queue://APP.JOBS --url "tcp://${INGRESS_URL}:443?sslEnabled=true&verifyHost=false&trustStorePath=${CERT_FOLDER}/broker.ks&trustStorePassword=000000"

Connection brokerURL = tcp://send-receive-sslacceptor-0-svc-ing-send-receive-project.demo.artemiscloud.io:443?sslEnabled=true&verifyHost=false&trustStorePath=/tmp/2876485925/broker.ks&trustStorePassword=000000
Consumer:: filter = null
Consumer ActiveMQQueue[APP.JOBS], thread=0 wait until 1000 messages are consumed
Received 1000
Consumer ActiveMQQueue[APP.JOBS], thread=0 Consumed: 1000 messages
Consumer ActiveMQQueue[APP.JOBS], thread=0 Elapsed time in second : 0 s
Consumer ActiveMQQueue[APP.JOBS], thread=0 Elapsed time in milli second : 107 milli seconds
Consumer ActiveMQQueue[APP.JOBS], thread=0 Consumed: 1000 messages
Consumer ActiveMQQueue[APP.JOBS], thread=0 Consumer thread finished
```

#### MULTICAST

For this use case, run first the consumer(s), then the producer.
More details there
https://activemq.apache.org/components/artemis/documentation/2.0.0/address-model.html.

In `n` other terminal(s) connect `n` consumer(s):

```{"stage":"test2", "rootdir":"$tmpdir.1/apache-artemis-2.36.0/bin/", "parallel":true, "runtime":"bash", "env":["INGRESS_URL", "CERT_FOLDER"], "label":"multicast: consume 1000 messages"}
$ ./artemis consumer --destination queue://APP.COMMANDS --url "tcp://${INGRESS_URL}:443?sslEnabled=true&verifyHost=false&trustStorePath=${CERT_FOLDER}/broker.ks&trustStorePassword=000000"

Connection brokerURL = tcp://send-receive-sslacceptor-0-svc-ing-send-receive-project.demo.artemiscloud.io:443?sslEnabled=true&verifyHost=false&trustStorePath=/tmp/2876485925/broker.ks&trustStorePassword=000000
Consumer:: filter = null
Consumer ActiveMQQueue[APP.COMMANDS], thread=0 wait until 1000 messages are consumed
Received 1000
Consumer ActiveMQQueue[APP.COMMANDS], thread=0 Consumed: 1000 messages
Consumer ActiveMQQueue[APP.COMMANDS], thread=0 Elapsed time in second : 15 s
Consumer ActiveMQQueue[APP.COMMANDS], thread=0 Elapsed time in milli second : 15101 milli seconds
Consumer ActiveMQQueue[APP.COMMANDS], thread=0 Consumed: 1000 messages
Consumer ActiveMQQueue[APP.COMMANDS], thread=0 Consumer thread finished
```

Then connect the producer to start broadcasting messages.

```{"stage":"test2", "rootdir":"$tmpdir.1/apache-artemis-2.36.0/bin/", "parallel":true, "runtime":"bash", "env":["INGRESS_URL", "CERT_FOLDER"], "label":"multicast: produce 1000 messages"}
$ sleep 5s
$ ./artemis producer --destination queue://APP.COMMANDS --url "tcp://${INGRESS_URL}:443?sslEnabled=true&verifyHost=false&trustStorePath=${CERT_FOLDER}/broker.ks&trustStorePassword=000000"

Connection brokerURL = tcp://send-receive-sslacceptor-0-svc-ing-send-receive-project.demo.artemiscloud.io:443?sslEnabled=true&verifyHost=false&trustStorePath=/tmp/2876485925/broker.ks&trustStorePassword=000000
Producer ActiveMQQueue[APP.COMMANDS], thread=0 Started to calculate elapsed time ...

Producer ActiveMQQueue[APP.COMMANDS], thread=0 Produced: 1000 messages
Producer ActiveMQQueue[APP.COMMANDS], thread=0 Elapsed time in second : 10 s
Producer ActiveMQQueue[APP.COMMANDS], thread=0 Elapsed time in milli second : 10146 milli seconds
```

### cleanup

To leave a pristine environment after executing this tutorial you can simply,
delete the minikube cluster and clean the `/etc/hosts` file.

```{"stage":"teardown", "requires":"init/minikube_start"}
$ minikube delete

* Deleting "minikube" in docker ...
* Deleting container "minikube" ...
* Removing /home/tlavocat/.minikube/machines/minikube ...
* Removed all traces of the "minikube" cluster.
```
