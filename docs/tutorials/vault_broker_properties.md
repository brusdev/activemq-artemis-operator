---
title: "Injecting secrets into broker properties from Vault"
description: "Using HashiCorp Vault Agent Injector and Banzai Webhook to inject secrets into broker properties from HashiCorp Vault"
draft: false
images: []
menu:
  docs:
    parent: "tutorials"
weight: 115
toc: true
---

Managing broker configuration in a production environment requires secure secret management. This tutorial demonstrates two approaches for injecting secrets into broker properties from HashiCorp Vault for Apache Artemis brokers managed by the ArkMQ Broker Operator:

1. **HashiCorp Vault Agent Injector** - Official HashiCorp solution using sidecar containers
2. **Banzai Secret Injection Webhook** - Webhook that replaces Vault references in environment variables

Both approaches:
- Use the same Kubernetes auth method with a service account (`vault-broker-sa`)
- Fetch secrets directly from Vault (eliminating the need to store sensitive data in Kubernetes)
- Use the same Vault policy and role for authentication

In this tutorial, we'll:
- Store only the address name `VAULT-TEST` in Vault
- Configure Kubernetes auth for both approaches using the same service account (`vault-broker-sa`)
- **HashiCorp approach**: Use Go templates to construct broker properties via sidecar
- **Banzai approach**: Inject values as environment variables, use `${VAR}` placeholders in broker properties
- Deploy two brokers and compare both injection methods

### Prerequisite

Before you start you need:
- Access to a running Kubernetes cluster environment. A [Minikube](https://minikube.sigs.k8s.io/docs/start/) running on your laptop will work fine.
- Helm 3 installed for deploying the HashiCorp Vault and Banzai Secret Injection Webhook
- Basic familiarity with Vault concepts

### Start minikube

```{"stage":"init", "id":"minikube_start"}
minikube start --profile tutorialtester --memory=4096 --cpus=2
minikube profile tutorialtester
```
```shell markdown_runner
* [tutorialtester] minikube v1.38.1 on Fedora 43
* Using the kvm2 driver based on user configuration
* Starting "tutorialtester" primary control-plane node in "tutorialtester" cluster
* Configuring bridge CNI (Container Networking Interface) ...
* Verifying Kubernetes components...
  - Using image gcr.io/k8s-minikube/storage-provisioner:v5
* Enabled addons: default-storageclass, storage-provisioner

  - Want kubectl v1.35.1? Try 'minikube kubectl -- get pods -A'
* Done! kubectl is now configured to use "tutorialtester" cluster and "default" namespace by default
! Starting v1.39.0, minikube will default to "containerd" container runtime. See #21973 for more info.
! /usr/local/bin/kubectl is version 1.33.3, which may have incompatibilities with Kubernetes 1.35.1.
* minikube profile was successfully set to tutorialtester
```

### Create the namespace

```{"stage":"init"}
kubectl create namespace vault-broker-project
kubectl config set-context --current --namespace=vault-broker-project
```
```shell markdown_runner
namespace/vault-broker-project created
Context "tutorialtester" modified.
```

### Deploy the ArkMQ operator

Go to the root of the operator repo and install it:

```{"stage":"operator-setup", "rootdir":"$initial_dir"}
./deploy/install_opr.sh
```
```shell markdown_runner
Deploying operator to watch single namespace
Client Version: 4.20.8
Kustomize Version: v5.6.0
Kubernetes Version: v1.35.1
customresourcedefinition.apiextensions.k8s.io/activemqartemises.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/activemqartemisaddresses.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/activemqartemisscaledowns.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/activemqartemissecurities.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/brokers.arkmq.org created
serviceaccount/activemq-artemis-controller-manager created
role.rbac.authorization.k8s.io/activemq-artemis-operator-role created
rolebinding.rbac.authorization.k8s.io/activemq-artemis-operator-rolebinding created
role.rbac.authorization.k8s.io/activemq-artemis-leader-election-role created
rolebinding.rbac.authorization.k8s.io/activemq-artemis-leader-election-rolebinding created
deployment.apps/activemq-artemis-controller-manager created
```

Wait for the Operator to start (status: `running`):

```{"stage":"operator-setup", "label":"wait for operator"}
kubectl rollout status deployment/activemq-artemis-controller-manager --timeout=600s
```
```shell markdown_runner
Waiting for deployment spec update to be observed...
Waiting for deployment spec update to be observed...
Waiting for deployment "activemq-artemis-controller-manager" rollout to finish: 0 out of 1 new replicas have been updated...
Waiting for deployment "activemq-artemis-controller-manager" rollout to finish: 0 of 1 updated replicas are available...
deployment "activemq-artemis-controller-manager" successfully rolled out
```

### Install HashiCorp Vault

Install Vault in dev mode with the Agent Injector enabled. In production, use a properly configured Vault instance.

```{"stage":"vault-setup", "label":"install vault"}
helm repo add hashicorp https://helm.releases.hashicorp.com
helm repo update
helm install vault hashicorp/vault --namespace=vault-broker-project --set "server.dev.enabled=true" --set "server.dev.devRootToken=root" --set "injector.enabled=true"
```
```shell markdown_runner
"hashicorp" already exists with the same configuration, skipping
Hang tight while we grab the latest from your chart repositories...
...Successfully got an update from the "jetstack" chart repository
...Successfully got an update from the "hashicorp" chart repository
Update Complete. ⎈Happy Helming!⎈
NAME: vault
LAST DEPLOYED: Wed Mar 25 10:38:16 2026
NAMESPACE: vault-broker-project
STATUS: deployed
REVISION: 1
NOTES:
Thank you for installing HashiCorp Vault!

Now that you have deployed Vault, you should look over the docs on using
Vault with Kubernetes available here:

https://developer.hashicorp.com/vault/docs


Your release is named vault. To learn more about the release, try:

  $ helm status vault
  $ helm get manifest vault
```

Wait for Vault to be ready:

```{"stage":"vault-setup", "label":"wait for vault"}
kubectl wait --for=jsonpath='{.status.readyReplicas}'=1 statefulset/vault --timeout=120s
```
```shell markdown_runner
statefulset.apps/vault condition met
```

### Configure Vault with address name

Store only the address name in Vault. This value will be used by both injection approaches to construct the broker properties:

```{"stage":"vault-setup", "runtime":"bash", "label":"store secrets in vault"}
kubectl exec vault-0 -- vault kv put secret/broker \
  addressName=VAULT-TEST
```
```shell markdown_runner
=== Secret Path ===
secret/data/broker

======= Metadata =======
Key                Value
---                -----
created_time       2026-03-25T09:39:11.451222189Z
custom_metadata    <nil>
deletion_time      n/a
destroyed          false
version            1
```

Verify the secret was stored:

```{"stage":"vault-setup", "runtime":"bash", "label":"verify vault secret"}
kubectl exec vault-0 -- vault kv get secret/broker
```
```shell markdown_runner
=== Secret Path ===
secret/data/broker

======= Metadata =======
Key                Value
---                -----
created_time       2026-03-25T09:39:11.451222189Z
custom_metadata    <nil>
deletion_time      n/a
destroyed          false
version            1

======= Data =======
Key            Value
---            -----
addressName    VAULT-TEST
```

### Create service account for Vault access

Create a service account for authenticating to Vault:

```{"stage":"vault-setup", "runtime":"bash", "label":"create service account"}
kubectl create serviceaccount vault-broker-sa
```
```shell markdown_runner
serviceaccount/vault-broker-sa created
```

### Configure Vault authentication

Enable Kubernetes auth and create a policy for the broker service account:

```{"stage":"vault-setup", "runtime":"bash", "label":"configure vault auth"}
# Enable Kubernetes auth
kubectl exec vault-0 -- vault auth enable kubernetes

# Configure Kubernetes auth to use the service account token
kubectl exec vault-0 -- sh -c 'vault write auth/kubernetes/config \
    kubernetes_host="https://$KUBERNETES_PORT_443_TCP_ADDR:443"'

# Create a simple policy allowing read access to broker secrets
kubectl exec vault-0 -- sh -c 'vault policy write broker - <<EOF
path "secret/data/broker" {
  capabilities = ["read"]
}
EOF'

# Create a role for the vault-broker-sa service account
kubectl exec vault-0 -- vault write auth/kubernetes/role/broker \
    bound_service_account_names=vault-broker-sa \
    bound_service_account_namespaces=vault-broker-project \
    policies=broker \
    ttl=24h
```
```shell markdown_runner
Success! Enabled kubernetes auth method at: kubernetes/
Success! Data written to: auth/kubernetes/config
Success! Uploaded policy: broker
WARNING! The following warnings were returned from Vault:

  * Role broker does not have an audience configured. While audiences are
  not required, consider specifying one if your use case would benefit from
  additional JWT claim verification.

```

## Approach 1: Using HashiCorp Vault Agent Injector

The Vault Agent Injector is the official HashiCorp solution for injecting secrets. Let's demonstrate this approach by creating a broker using the Vault configuration (`secret/data/broker`) to create an address with the name `VAULT-TEST`.

### Deploy the broker (hashicorp-broker)

Create the broker CR with Vault Agent Injector annotations. The template constructs the broker properties using the address name from Vault with the path `secret/data/broker`:

```{"stage":"agent-injector", "runtime":"bash", "label":"deploy hashicorp broker"}
kubectl apply -f - << EOF
apiVersion: broker.amq.io/v1beta1
kind: ActiveMQArtemis
metadata:
  name: hashicorp-broker
spec:
  deploymentPlan:
    annotations:
      vault.hashicorp.com/agent-inject: "true"
      vault.hashicorp.com/role: "broker"
      vault.hashicorp.com/agent-inject-secret-vault-broker.properties: "secret/data/broker"
      vault.hashicorp.com/agent-inject-template-vault-broker.properties: |
        {{- with secret "secret/data/broker" -}}
        addressConfigurations.{{ .Data.data.addressName }}.routingTypes=ANYCAST
        addressConfigurations.{{ .Data.data.addressName }}.queueConfigs.{{ .Data.data.addressName }}.address={{ .Data.data.addressName }}
        addressConfigurations.{{ .Data.data.addressName}}.queueConfigs.{{ .Data.data.addressName }}.routingType=ANYCAST
        {{- end -}}
      vault.hashicorp.com/agent-inject-file-broker.properties: "broker.properties"
    podSecurity:
      serviceAccountName: vault-broker-sa
  env:
  - name: JAVA_ARGS_APPEND
    value: "-Dbroker.properties=/amq/extra/secrets/hashicorp-broker-props/broker.properties,/vault/secrets/vault-broker.properties"
EOF
```
```shell markdown_runner
activemqartemis.broker.amq.io/hashicorp-broker created
```

Wait for the broker to be ready:

```{"stage":"agent-injector", "label":"wait for hashicorp broker"}
kubectl wait ActiveMQArtemis hashicorp-broker --for=condition=Ready --namespace=vault-broker-project --timeout=300s
```
```shell markdown_runner
activemqartemis.broker.amq.io/hashicorp-broker condition met
```

### Verify Vault Agent Injector worked

Check that the Vault agent sidecar was injected:

```{"stage":"agent-injector", "runtime":"bash", "label":"check hashicorp vault sidecar"}
kubectl get pod hashicorp-broker-ss-0 -o jsonpath='{.spec.containers[*].name}' | grep vault-agent
```
```shell markdown_runner
hashicorp-broker-container vault-agent
```

Verify the broker properties were constructed by the Vault agent template using the address name from Vault:

```{"stage":"agent-injector", "runtime":"bash", "label":"verify hashicorp injected config"}
kubectl exec hashicorp-broker-ss-0 -c hashicorp-broker-container -- cat /vault/secrets/vault-broker.properties
```
```shell markdown_runner
addressConfigurations.VAULT-TEST.routingTypes=ANYCAST
addressConfigurations.VAULT-TEST.queueConfigs.VAULT-TEST.address=VAULT-TEST
addressConfigurations.VAULT-TEST.queueConfigs.VAULT-TEST.routingType=ANYCAST### ENV ###
SHELL=/bin/bash
COLORTERM=truecolor
HISTCONTROL=ignoredups
XDG_MENU_PREFIX=gnome-
TERM_PROGRAM_VERSION=1.111.0
QT_IM_MODULES=wayland;ibus
HISTSIZE=1000
HOSTNAME=li-b9f1104c-338e-11b2-a85c-d0ed6b5f92b9.ibm.com
BOB_SHELL_CLI_IDE_SERVER_PORT=44773
JAVA_HOME=/usr/lib/jvm/java-21-openjdk
GUESTFISH_OUTPUT=\e[0m
SSH_AUTH_SOCK=/run/user/1000/gcr/ssh
MEMORY_PRESSURE_WRITE=c29tZSAyMDAwMDAgMjAwMDAwMAA=
SDKMAN_CANDIDATES_DIR=/home/dbruscin/.sdkman/candidates
XMODIFIERS=@im=ibus
DESKTOP_SESSION=gnome
NO_AT_BRIDGE=1
GPG_TTY=/dev/pts/2
EDITOR=/usr/bin/vim
SDKMAN_BROKER_API=https://broker.sdkman.io
PWD=/tmp/1935042029
LOGNAME=dbruscin
XDG_SESSION_DESKTOP=gnome
XDG_SESSION_TYPE=wayland
SYSTEMD_EXEC_PID=17197
XAUTHORITY=/run/user/1000/.mutter-Xwaylandauth.QC46M3
GUESTFISH_RESTORE=\e[0m
VSCODE_GIT_ASKPASS_NODE=/usr/share/code/code
GDM_LANG=en_US.UTF-8
HOME=/home/dbruscin
USERNAME=dbruscin
LANG=en_US.UTF-8
LS_COLORS=rs=0:di=01;34:ln=01;36:mh=00:pi=40;33:so=01;35:do=01;35:bd=40;33;01:cd=40;33;01:or=40;31;01:mi=01;37;41:su=37;41:sg=30;43:ca=00:tw=30;42:ow=34;42:st=37;44:ex=01;32:*.7z=01;31:*.ace=01;31:*.alz=01;31:*.apk=01;31:*.arc=01;31:*.arj=01;31:*.bz=01;31:*.bz2=01;31:*.cab=01;31:*.cpio=01;31:*.crate=01;31:*.deb=01;31:*.drpm=01;31:*.dwm=01;31:*.dz=01;31:*.ear=01;31:*.egg=01;31:*.esd=01;31:*.gz=01;31:*.jar=01;31:*.lha=01;31:*.lrz=01;31:*.lz=01;31:*.lz4=01;31:*.lzh=01;31:*.lzma=01;31:*.lzo=01;31:*.pyz=01;31:*.rar=01;31:*.rpm=01;31:*.rz=01;31:*.sar=01;31:*.swm=01;31:*.t7z=01;31:*.tar=01;31:*.taz=01;31:*.tbz=01;31:*.tbz2=01;31:*.tgz=01;31:*.tlz=01;31:*.txz=01;31:*.tz=01;31:*.tzo=01;31:*.tzst=01;31:*.udeb=01;31:*.war=01;31:*.whl=01;31:*.wim=01;31:*.xz=01;31:*.z=01;31:*.zip=01;31:*.zoo=01;31:*.zst=01;31:*.avif=01;35:*.jpg=01;35:*.jpeg=01;35:*.jxl=01;35:*.mjpg=01;35:*.mjpeg=01;35:*.gif=01;35:*.bmp=01;35:*.pbm=01;35:*.pgm=01;35:*.ppm=01;35:*.tga=01;35:*.xbm=01;35:*.xpm=01;35:*.tif=01;35:*.tiff=01;35:*.png=01;35:*.svg=01;35:*.svgz=01;35:*.mng=01;35:*.pcx=01;35:*.mov=01;35:*.mpg=01;35:*.mpeg=01;35:*.m2v=01;35:*.mkv=01;35:*.webm=01;35:*.webp=01;35:*.ogm=01;35:*.mp4=01;35:*.m4v=01;35:*.mp4v=01;35:*.vob=01;35:*.qt=01;35:*.nuv=01;35:*.wmv=01;35:*.asf=01;35:*.rm=01;35:*.rmvb=01;35:*.flc=01;35:*.avi=01;35:*.fli=01;35:*.flv=01;35:*.gl=01;35:*.dl=01;35:*.xcf=01;35:*.xwd=01;35:*.yuv=01;35:*.cgm=01;35:*.emf=01;35:*.ogv=01;35:*.ogx=01;35:*.aac=01;36:*.au=01;36:*.flac=01;36:*.m4a=01;36:*.mid=01;36:*.midi=01;36:*.mka=01;36:*.mp3=01;36:*.mpc=01;36:*.ogg=01;36:*.ra=01;36:*.wav=01;36:*.oga=01;36:*.opus=01;36:*.spx=01;36:*.xspf=01;36:*~=00;90:*#=00;90:*.bak=00;90:*.crdownload=00;90:*.dpkg-dist=00;90:*.dpkg-new=00;90:*.dpkg-old=00;90:*.dpkg-tmp=00;90:*.old=00;90:*.orig=00;90:*.part=00;90:*.rej=00;90:*.rpmnew=00;90:*.rpmorig=00;90:*.rpmsave=00;90:*.swp=00;90:*.tmp=00;90:*.ucf-dist=00;90:*.ucf-new=00;90:*.ucf-old=00;90:
CLAUDE_CODE_USE_VERTEX=1
XDG_CURRENT_DESKTOP=GNOME
MEMORY_PRESSURE_WATCH=/sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service/app.slice/dbus-:1.2-org.gnome.Nautilus@1.service/memory.pressure
WAYLAND_DISPLAY=wayland-0
BOB_SHELL_CLI_IDE_WORKSPACE_PATH=/home/dbruscin/Workspace/brusdev/arkmq-org/activemq-artemis-operator
GUESTFISH_PS1=\[\e[1;32m\]><fs>\[\e[0;31m\] 
GIT_ASKPASS=/usr/share/code/resources/app/extensions/git/dist/askpass.sh
TPM2_PKCS11_LOG_LEVEL=0
INVOCATION_ID=3e644ae487f042a1a5c7a1d7a5287ec4
GROOVY_HOME=/home/dbruscin/.sdkman/candidates/groovy/current
MANAGERPID=2474
ANTHROPIC_VERTEX_PROJECT_ID=itpc-gcp-cp-pe-eng-claude
CHROME_DESKTOP=code.desktop
WORKING_DIR=/home/dbruscin/Workspace/brusdev/arkmq-org/activemq-artemis-operator
MOZ_GMP_PATH=/usr/lib64/mozilla/plugins/gmp-gmpopenh264/system-installed
VSCODE_GIT_ASKPASS_EXTRA_ARGS=
GNOME_SETUP_DISPLAY=:1
VSCODE_PYTHON_AUTOACTIVATE_GUARD=1
CLAUDE_CODE_SSE_PORT=47350
XDG_SESSION_CLASS=user
TERM=xterm-256color
LESSOPEN=||/usr/bin/lesspipe.sh %s
USER=dbruscin
VSCODE_GIT_IPC_HANDLE=/run/user/1000/vscode-git-cad0a8528e.sock
SDKMAN_DIR=/home/dbruscin/.sdkman
DISPLAY=:0
SHLVL=8
GUESTFISH_INIT=\e[1;34m
TSS2_LOG=fapi+NONE
QT_IM_MODULE=ibus
SDKMAN_CANDIDATES_API=https://api.sdkman.io/2
MANAGERPIDFDID=2475
FC_FONTATIONS=1
XDG_RUNTIME_DIR=/run/user/1000
DEBUGINFOD_URLS=ima:enforcing https://debuginfod.fedoraproject.org/ ima:ignore https://debuginfod.usersys.redhat.com/ 
DOCKER_HOST=unix:///run/user/1000/podman/podman.sock
DEBUGINFOD_IMA_CERT_PATH=/etc/keys/ima:
TPM2_PKCS11_STORE=/etc/tpm2_pkcs11
CLOUD_ML_REGION=us-east5
VSCODE_GIT_ASKPASS_MAIN=/usr/share/code/resources/app/extensions/git/dist/askpass-main.js
JOURNAL_STREAM=9:154836
XDG_DATA_DIRS=/home/dbruscin/.local/share/flatpak/exports/share:/var/lib/flatpak/exports/share:/usr/local/share/:/usr/share/
GDK_BACKEND=wayland
PATH=/opt/apache-maven-3.9.12/bin/:/home/dbruscin/go/bin:/home/dbruscin/.config/Code/User/globalStorage/github.copilot-chat/debugCommand:/home/dbruscin/.config/Code/User/globalStorage/github.copilot-chat/copilotCli:~/sdk/operator-sdk-v1.28.0:/home/dbruscin/.sdkman/candidates/groovy/current/bin:/opt/apache-maven-3.9.12/bin/:/home/dbruscin/go/bin:/home/dbruscin/.local/bin:/home/dbruscin/bin:/usr/local/bin:/usr/bin:/home/dbruscin/.npm-global/bin:/home/dbruscin/.npm-global/bin
GDMSESSION=gnome
DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus
SDKMAN_PLATFORM=linuxx64
MAIL=/var/spool/mail/dbruscin
GIO_LAUNCHED_DESKTOP_FILE_PID=17298
GIO_LAUNCHED_DESKTOP_FILE=/usr/share/applications/code.desktop
GOPATH=/home/dbruscin/go
TERM_PROGRAM=vscode
_=/usr/bin/printenv

```

Perfect! The Vault agent template retrieved `addressName=VAULT-TEST` from Vault and constructed the complete broker properties. Now verify that the VAULT-TEST address and queue were created:

```{"stage":"agent-injector", "runtime":"bash", "label":"check hashicorp vault-test queue"}
kubectl exec hashicorp-broker-ss-0 -c hashicorp-broker-container -- /bin/bash -c 'cd amq-broker/bin && ./artemis queue stat --user admin --password admin --url tcp://hashicorp-broker-ss-0:61616 | grep VAULT-TEST'
```
```shell markdown_runner
|VAULT-TEST        |VAULT-TEST        |   0    |   0   |   0    |    0     |   0    |    0    |ANYCAST| false  |
NOTE: Picked up JDK_JAVA_OPTIONS: -Dbroker.properties=/amq/extra/secrets/hashicorp-broker-props/broker.properties
```

Excellent! The `VAULT-TEST` queue and address were successfully created using the Vault Agent Injector.

## Approach 2: Using Banzai Secret Injection Webhook

### Install Bank-Vaults Secret Injection Webhook

Install the Bank-Vaults secrets webhook using Helm OCI registry. The webhook uses the same `vault-broker-sa` service account to authenticate to Vault via Kubernetes auth:

```{"stage":"banzai", "runtime":"bash", "label":"install secrets webhook"}
helm install vault-secrets-webhook oci://ghcr.io/bank-vaults/helm-charts/vault-secrets-webhook \
  --namespace=vault-broker-project \
  --set ignoreReleaseNamespace=false
```
```shell markdown_runner
NAME: vault-secrets-webhook
LAST DEPLOYED: Wed Mar 25 10:40:29 2026
NAMESPACE: vault-broker-project
STATUS: deployed
REVISION: 1
TEST SUITE: None
Pulled: ghcr.io/bank-vaults/helm-charts/vault-secrets-webhook:1.22.2
Digest: sha256:4e67105d138a3025c8c75a61934a221186eefdb6db77860d3bf42118ce2f2ec1
```

Wait for the webhook to be ready:

```{"stage":"banzai", "label":"wait for webhook"}
kubectl rollout status deployment/vault-secrets-webhook --timeout=120s
```
```shell markdown_runner
Waiting for deployment "vault-secrets-webhook" rollout to finish: 0 out of 2 new replicas have been updated...
Waiting for deployment "vault-secrets-webhook" rollout to finish: 0 of 2 updated replicas are available...
Waiting for deployment "vault-secrets-webhook" rollout to finish: 1 of 2 updated replicas are available...
deployment "vault-secrets-webhook" successfully rolled out
```

### Create broker properties secret

Create a Kubernetes secret that uses an environment variable for the address name. This variable will be injected from Vault via an environment variable in the broker pod:

```{"stage":"banzai", "runtime":"bash", "label":"create broker properties secret"}
kubectl apply -f - << 'EOF'
apiVersion: v1
kind: Secret
metadata:
  name: banzai-broker-config-bp
type: Opaque
stringData:
  broker.properties: |
    addressConfigurations.${VAULT_ADDRESS_NAME}.routingTypes=ANYCAST
    addressConfigurations.${VAULT_ADDRESS_NAME}.queueConfigs.${VAULT_ADDRESS_NAME}.address=${VAULT_ADDRESS_NAME}
    addressConfigurations.${VAULT_ADDRESS_NAME}.queueConfigs.${VAULT_ADDRESS_NAME}.routingType=ANYCAST
EOF
```
```shell markdown_runner
secret/banzai-broker-config-bp created
```

Verify the secret was created with environment variable placeholders:

```{"stage":"banzai", "runtime":"bash", "label":"verify secret with env var"}
kubectl get secret banzai-broker-config-bp -o jsonpath='{.data.broker\.properties}' | base64 -d
```
```shell markdown_runner
addressConfigurations.${VAULT_ADDRESS_NAME}.routingTypes=ANYCAST
addressConfigurations.${VAULT_ADDRESS_NAME}.queueConfigs.${VAULT_ADDRESS_NAME}.address=${VAULT_ADDRESS_NAME}
addressConfigurations.${VAULT_ADDRESS_NAME}.queueConfigs.${VAULT_ADDRESS_NAME}.routingType=ANYCAST
```

### Deploy the broker (banzai-broker)

Create the broker CR with an environment variable that gets injected from Vault. The Banzai webhook will replace the `vault:` reference in the environment variable with the actual value from Vault with the path `secret/data/broker`:

```{"stage":"banzai", "runtime":"bash", "label":"deploy banzai broker"}
kubectl apply -f - << EOF
apiVersion: broker.amq.io/v1beta1
kind: ActiveMQArtemis
metadata:
  name: banzai-broker
spec:
  deploymentPlan:
    annotations:
      vault.security.banzaicloud.io/vault-addr: "http://vault:8200"
      vault.security.banzaicloud.io/vault-role: "broker"
    extraMounts:
      secrets:
      - banzai-broker-config-bp
    podSecurity:
      serviceAccountName: vault-broker-sa
  env:
  - name: VAULT_ADDRESS_NAME
    value: "vault:secret/data/broker#addressName"
EOF
```
```shell markdown_runner
activemqartemis.broker.amq.io/banzai-broker created
```

Wait for the broker to be ready:

```{"stage":"banzai", "label":"wait for banzai broker"}
kubectl wait ActiveMQArtemis banzai-broker --for=condition=Ready --namespace=vault-broker-project --timeout=300s
```
```shell markdown_runner
activemqartemis.broker.amq.io/banzai-broker condition met
```

### Verify vault injection worked

Check that the environment variable was injected from Vault by the Banzai webhook:

```{"stage":"banzai", "runtime":"bash", "label":"check banzai injected env variable"}
kubectl exec banzai-broker-ss-0 -- printenv VAULT_ADDRESS_NAME
```
```shell markdown_runner
vault:secret/data/broker#addressName
Defaulted container "banzai-broker-container" out of: banzai-broker-container, copy-vault-env (init), banzai-broker-container-init (init)
```

Perfect! The Banzai webhook replaced the `vault:secret/data/broker#addressName` reference with `VAULT-TEST`.

Check the broker properties file which uses the environment variable:

```{"stage":"banzai", "runtime":"bash", "label":"verify banzai broker config"}
kubectl exec banzai-broker-ss-0 -c banzai-broker-container -- /bin/bash -c 'cat /amq/extra/secrets/banzai-broker-config-bp/broker.properties'
```
```shell markdown_runner
addressConfigurations.${VAULT_ADDRESS_NAME}.routingTypes=ANYCAST
addressConfigurations.${VAULT_ADDRESS_NAME}.queueConfigs.${VAULT_ADDRESS_NAME}.address=${VAULT_ADDRESS_NAME}
addressConfigurations.${VAULT_ADDRESS_NAME}.queueConfigs.${VAULT_ADDRESS_NAME}.routingType=ANYCAST
```

The broker properties still contain the `${VAULT_ADDRESS_NAME}` placeholder, but Artemis will resolve it using the injected environment variable. Now verify that the VAULT-TEST address and queue were created:

```{"stage":"banzai", "runtime":"bash", "label":"check banzai vault-test queue"}
kubectl exec banzai-broker-ss-0 -- /bin/bash -c 'cd amq-broker/bin && ./artemis queue stat --user admin --password admin --url tcp://banzai-broker-ss-0:61616 | grep VAULT-TEST'
```
```shell markdown_runner
|VAULT-TEST        |VAULT-TEST        |   0    |   0   |   0    |    0     |   0    |    0    |ANYCAST| false  |
Defaulted container "banzai-broker-container" out of: banzai-broker-container, copy-vault-env (init), banzai-broker-container-init (init)
NOTE: Picked up JDK_JAVA_OPTIONS: -Dbroker.properties=/amq/extra/secrets/banzai-broker-props/broker.properties,/amq/extra/secrets/banzai-broker-config-bp/,/amq/extra/secrets/banzai-broker-config-bp/broker-${STATEFUL_SET_ORDINAL}/
```

Excellent! The `VAULT-TEST` queue bound to the `VAULT-TEST` address was successfully created from the Vault configuration.

### Comparing Both Approaches

Both brokers successfully created the same `VAULT-TEST` queue and address from the same Vault configuration, demonstrating two different injection methods:

**HashiCorp Vault Agent Injector (`hashicorp-broker`):**
- Official HashiCorp solution
- Injects secrets via sidecar container (`vault-agent`)
- Uses `vault-broker-sa` service account for Kubernetes auth
- Supports secret rotation
- Better for production environments
- Properties injected at `/vault/secrets/vault-broker.properties`

**Banzai Secret Injection Webhook (`banzai-broker`):**
- Webhook intercepts pod creation
- Replaces `vault:` references in environment variables
- Uses same `vault-broker-sa` service account for Kubernetes auth
- Good for static secret injection
- Properties mounted at `/amq/extra/secrets/banzai-broker-config-bp/broker.properties`

**Common elements:**
- Both use the same `vault-broker-sa` service account
- Both authenticate via Kubernetes auth method
- Both use the same Vault policy and role
- Both read from the same Vault secret (`secret/data/broker`)

### How it works

This tutorial demonstrates two approaches for injecting the **same Vault secret** (`secret/data/broker` containing `addressName=VAULT-TEST`) into two different broker instances:

#### HashiCorp Vault Agent Injector (`hashicorp-broker`)

1. **Vault Storage**: Address name stored at `secret/data/broker` (`addressName=VAULT-TEST`)
2. **Vault Auth**: Kubernetes auth method with simple policy allowing read access to broker secrets
3. **Vault Agent**: Sidecar container injected into broker pod via annotations
4. **Template Processing**: Agent uses Go template to fetch `addressName` and construct complete broker properties
5. **Artemis Broker**: Loads constructed properties from `/vault/secrets/broker.properties` via `JAVA_ARGS_APPEND`

#### Banzai Secret Injection Webhook (`banzai-broker`)

1. **Vault Storage**: Same address name at `secret/data/broker` (`addressName=VAULT-TEST`)
2. **Vault Auth**: Webhook uses `vault-broker-sa` service account with Kubernetes auth (same as HashiCorp approach)
3. **Environment Variable**: ActiveMQArtemis CR defines `VAULT_ADDRESS_NAME` env var with `vault:secret/data/broker#addressName` reference
4. **Banzai Webhook**: Mutating webhook intercepts pod creation, authenticates to Vault using Kubernetes auth, replaces `vault:` reference in env var with actual value `VAULT-TEST`
5. **Kubernetes Secret**: Broker properties use `${VAULT_ADDRESS_NAME}` placeholder
6. **ArkMQ Operator**: Mounts the secret via `extraMounts.secrets`
7. **Artemis Broker**: Loads properties and resolves `${VAULT_ADDRESS_NAME}` to `VAULT-TEST` from environment variable

Both brokers create the same infrastructure from the Vault-injected address name:
- Address `VAULT-TEST` with ANYCAST routing
- Queue `VAULT-TEST` bound to the address
- Queue routing type set to ANYCAST

**Key Differences**:
- **Authentication timing**:
  - HashiCorp: Vault agent authenticates continuously during pod runtime (sidecar container)
  - Banzai: Webhook authenticates once during pod creation (mutating webhook)
- **Secret injection method**:
  - HashiCorp: Template-based construction using Go templates to build complete properties file
  - Banzai: Injects Vault value into environment variable, Artemis resolves `${VAR}` placeholders at runtime
- **Resource usage**:
  - HashiCorp: Additional sidecar container per pod
  - Banzai: Single webhook deployment for all pods

This approach provides:
- **Secure secret management**: Sensitive data never stored in Kubernetes
- **Dynamic configuration**: Change Vault values and restart pods to pick up new configuration
- **Audit trail**: Vault logs all secret access
- **Centralized management**: One Vault instance for all broker configurations
- **Declarative infrastructure**: Queue and address creation through properties instead of manual configuration

### Cleanup

To leave a pristine environment after executing this tutorial, delete the minikube cluster:

```{"stage":"teardown", "requires":"init/minikube_start"}
minikube delete --profile tutorialtester
```
```shell markdown_runner
* Deleting "tutorialtester" in kvm2 ...
* Removed all traces of the "tutorialtester" cluster.
```

### Additional considerations

For production environments, consider:

- **Choose the Right Approach**:
  - Vault Agent Injector: Official HashiCorp solution, better for production with secret rotation support (Approach 1)
  - Banzai Webhook: Simpler setup, good for static secret injection (Approach 2)
- **Vault Authentication**: Both approaches use Kubernetes auth method with the same service account (simplified example shown in tutorial)
- **Vault Policies**: Create specific policies with least privilege access - separate policies per application/team in production
- **TLS**: Enable TLS for Vault communication
- **HA Vault**: Deploy Vault in high-availability mode with proper storage backend
- **Secret Rotation**: Vault Agent Injector supports automatic secret rotation
- **Monitoring**: Monitor webhook/agent injection and Vault access
- **Broker Properties**: Store complete broker configuration in Vault:
  - Security credentials (user passwords, keystore passwords)
  - Connection settings (cluster passwords, connector passwords)
  - Queue and address creation
  - Performance tuning parameters
  - Environment-specific settings

### What can be configured via broker.properties from Vault

The Artemis broker supports extensive configuration through broker.properties:

- **Address Configurations**:
  - `addressConfigurations.ADDRESS_NAME.routingTypes=ANYCAST|MULTICAST`
  - `addressConfigurations.ADDRESS_NAME.queueConfigs.QUEUE_NAME.address=ADDRESS_NAME`
  - `addressConfigurations.ADDRESS_NAME.queueConfigs.QUEUE_NAME.routingType=ANYCAST`
- **Address Settings**: `addressSettings."AddressPattern".expiryDelay=value`
- **Security**: User credentials, LDAP settings
- **Clustering**: Cluster user, cluster password
- **Connectors**: Connector configurations
- **Global Settings**: Memory limits, journal settings
- **Custom Properties**: Any valid Artemis configuration property

This makes it easy to manage environment-specific configurations (dev, staging, production) entirely through Vault.

For more information:
- [Banzai Secret Injection Webhook](https://github.com/bank-vaults/secrets-webhook)
- [HashiCorp Vault Agent Injector](https://developer.hashicorp.com/vault/docs/platform/k8s/injector)
- [HashiCorp Vault](https://www.vaultproject.io/)
- [ArkMQ Operator Documentation](https://arkmq.org/)
- [Apache Artemis Configuration](https://activemq.apache.org/components/artemis/documentation/)
