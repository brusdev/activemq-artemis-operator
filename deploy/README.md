## Manual deployment of Artemis Operator
Resource files specified in this directory are necessary for manual deployment of Artemis Operator.
You have an option to deploy Artemis Operator managing only resources in its own deployed namespace, or you can make it
manage all resources across multiple namespaces if you have cluster wide admin access.
For single namespace usage you should move files `060_cluster_role` and `070_cluster_role_binding` out of `install` directory or you can see deployment permission issues.
For cluster-wide Operator management of artemis components you would need to update `WATCH_NAMESPACE` value.
We numbered files for cases, when they need to be manually applied one by one.

## Operator watching single namespace

Deploy whole `install` folder, except 2 files. Move out of that folder `060_cluster_role` and `070_cluster_role_binding` files.

```shell
mv deploy/install/*_cluster_role*yaml .
kubectl create -f deploy/install # -n <namespace>
```

Alternatively you can use script to deploy it for you
```shell
./deploy/install_opr.sh
```

## Operator watching all namespaces

Change value of **WATCH_NAMESPACE** environment variable in `110_operator.yaml` file to `"*"` or empty string (see example).
You should be fine with deploying whole `install` folder as is, for cluster wide Artemis Operator deployment.

```yaml
        - name: WATCH_NAMESPACE
          value: "*"
```

```shell
kubectl create -f deploy/install # -n <namespace>
```

And change the subjects `<namespace>` to match your target namespace in `070_cluster_role_binding.yaml` file using command:
```shell
sed -i 's/namespace: .*/namespace: <namespace>/' install/070_cluster_role_binding.yaml
```

Alternatively you can use script to deploy it for you
```shell
./deploy/cluster_wide_install_opr.sh
```

## Undeploy Operator
 
To undeploy deployed Operator using these deploy yaml files, just execute following command:
```shell
kubectl delete -f deploy/install # -n <namespace>
```

Or use simple script
```shell
./deploy/undeploy_all.sh
```

### What are these yaml files in deploy folder

These yaml files serve for manual deployment of Artemis Operator. 
They are generated from the *generate-deploy* make target located in 
[ActiveMQ Artemis Cloud](https://github.com/artemiscloud/activemq-artemis-operator) project.

#### Note ####

If you make any changes to the CRD definitions or any other config resources, you need to regenerate these YAML files 
by run the following command from the project root.

```
make generate-deploy
```
<<<<<<< HEAD
=======

# How to use the yamls in this dir to deploy an operator

## To deploy an operator watching single namespace (operator's namespace)

Assuming the target namespace is current namespace (if not use kubectl's -n option to specify the target namespace)

1. Deploy all the crds
```
kubectl create -f ./crds
```

2. Deploy operator

You need to deploy all the yamls from this dir except *cluster_role.yaml* and *cluster_role_binding.yaml*:
```
kubectl create -f ./service_account.yaml
kubectl create -f ./role.yaml
kubectl create -f ./role_binding.yaml
kubectl create -f ./election_role.yaml
kubectl create -f ./election_role_binding.yaml
kubectl create -f ./operator_config.yaml
kubectl create -f ./operator.yaml
```

## To deploy an operator watching all namespace

The steps are similar to those for single namespace operators except that you replace the *role.yaml* and *role_binding.yaml* with *cluster_role.yaml* and *cluster_role_binding.yaml* respectively.

**Note**
Before deploy, you should edit operator.yaml and change the **WATCH_NAMESPACE** env var value to be empty string
and also change the subjects namespace to match your target namespace in cluster_role_binding.yaml
as illustrated in following

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: activemq-artemis-manager-rolebinding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: activemq-artemis-activemq-artemis-operator
subjects:
- kind: ServiceAccount
  name: activemq-artemis-controller-manager
  namespace: <<This must match your operator's target namespace>>
```

After making the above changes deploy the operator as follows
(Assuming the target namespace is current namespace. You can use kubectl's -n option to specify otherwise)

```
kubectl create -f ./crds
kubectl create -f ./service_account.yaml
kubectl create -f ./role_binding.yaml
kubectl create -f ./role.yaml
kubectl create -f ./cluster_role.yaml
kubectl create -f ./cluster_role_binding.yaml
kubectl create -f ./election_role.yaml
kubectl create -f ./election_role_binding.yaml
kubectl create -f ./operator_config.yaml
kubectl create -f ./operator.yaml
```
>>>>>>> c1899bb9 (Fix relative path on kubectl create)
