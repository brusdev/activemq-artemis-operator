/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
// +kubebuilder:docs-gen:collapse=Apache License

package controllers

import (
	"encoding/json"
	"os"

	brokerv1beta1 "github.com/artemiscloud/activemq-artemis-operator/api/v1beta1"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/certutil"
	"github.com/artemiscloud/activemq-artemis-operator/pkg/utils/common"
	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	brokerCrNameBase = "broker-cert-mgr"

	rootIssuerName       = "root-issuer"
	rootCertName         = "root-cert"
	rootCertNamespce     = "cert-manager"
	rootCertSecretName   = "artemis-root-cert-secret"
	caIssuerName         = "broker-ca-issuer"
	caPemTrustStoreName  = "ca-truststore.pem"
	caTrustStorePassword = "changeit"
)

var (
	serverCert   = "server-cert"
	rootIssuer   = &cmv1.ClusterIssuer{}
	rootCert     = &cmv1.Certificate{}
	caIssuer     = &cmv1.ClusterIssuer{}
	caBundleName = "ca-bundle"
)

type ConnectorConfig struct {
	Name             string
	FactoryClassName string
	Params           map[string]string
}

var _ = Describe("artemis controller with cert manager test", Label("controller-cert-mgr-test"), func() {
	var installedCertManager bool = false

	BeforeEach(func() {
		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			//if cert manager/trust manager is not installed, install it
			if !CertManagerInstalled() {
				Expect(InstallCertManager()).To(Succeed())
				installedCertManager = true
			}

			rootIssuer = InstallClusteredIssuer(rootIssuerName, nil)

			rootCert = InstallCert(rootCertName, rootCertNamespce, func(candidate *cmv1.Certificate) {
				candidate.Spec.IsCA = true
				candidate.Spec.CommonName = "artemis.root.ca"
				candidate.Spec.SecretName = rootCertSecretName
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: rootIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			caIssuer = InstallClusteredIssuer(caIssuerName, func(candidate *cmv1.ClusterIssuer) {
				candidate.Spec.SelfSigned = nil
				candidate.Spec.CA = &cmv1.CAIssuer{
					SecretName: rootCertSecretName,
				}
			})
			InstallCaBundle(caBundleName, rootCertSecretName, caPemTrustStoreName)
		}
	})

	AfterEach(func() {
		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			UnInstallCaBundle(caBundleName)
			UninstallClusteredIssuer(caIssuerName)
			UninstallCert(rootCert.Name, rootCert.Namespace)
			UninstallClusteredIssuer(rootIssuerName)

			if installedCertManager {
				Expect(UninstallCertManager()).To(Succeed())
				installedCertManager = false
			}
		}
	})

	Describe("cert manager integration test", func() {
		Context("tls exposure with cert manager", func() {
			BeforeEach(func() {
				if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
					InstallCert(serverCert, defaultNamespace, func(candidate *cmv1.Certificate) {
						candidate.Spec.DNSNames = []string{brokerCrNameBase + "0-ss-0", brokerCrNameBase + "1-ss-0", brokerCrNameBase + "2-ss-0"}
						candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
							Name: caIssuer.Name,
							Kind: "ClusterIssuer",
						}
					})
				}
			})
			AfterEach(func() {
				if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
					UninstallCert(serverCert, defaultNamespace)
				}
			})
			It("test configured with cert and ca bundle", func() {
				if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
					testConfiguredWithCertAndBundle(serverCert+"-secret", caBundleName)
				}
			})
		})
		Context("certutil functions", Label("check-cert-secret"), func() {
			It("certutil - is secret from cert", func() {
				secret := corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mysecret",
					},
					Data: map[string][]byte{
						"ca.crt":  []byte("some cert"),
						"tls.crt": []byte("some cert"),
					},
				}
				ok, valid := certutil.IsSecretFromCert(&secret)
				Expect(ok).To(BeFalse())
				Expect(valid).To(BeFalse())

				secret.ObjectMeta.Annotations = map[string]string{
					certutil.Cert_annotation_key: "caissuer",
				}
				ok, valid = certutil.IsSecretFromCert(&secret)
				Expect(ok).To(BeTrue())
				Expect(valid).To(BeFalse())

				secret.Data["tls.key"] = []byte("somekey")
				ok, valid = certutil.IsSecretFromCert(&secret)
				Expect(ok).To(BeTrue())
				Expect(valid).To(BeTrue())
			})
		})

		Context("certificate rotation", Label("certificate"), func() {
			It("broker certificate rotation", func() {
				activeMQArtemis := generateArtemisSpec(defaultNamespace)

				issuerName := activeMQArtemis.Name + "-issuer"
				By("Creating issuer: " + issuerName)
				issuer := cmv1.Issuer{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "Issuer",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      issuerName,
						Namespace: defaultNamespace,
					},
					Spec: cmv1.IssuerSpec{
						IssuerConfig: cmv1.IssuerConfig{
							SelfSigned: &cmv1.SelfSignedIssuer{},
						},
					},
				}
				Expect(k8sClient.Create(ctx, &issuer)).Should(Succeed())

				certName := activeMQArtemis.Name + "-cert"
				certSecretName := certName + "-secret"
				By("Creating certificate: " + certName)
				cert := cmv1.Certificate{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "Certificate",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      certName,
						Namespace: defaultNamespace,
					},
					Spec: cmv1.CertificateSpec{
						SecretName: certSecretName,
						DNSNames:   defaultSanDnsNames,
						IssuerRef: cmmetav1.ObjectReference{
							Name: issuerName,
							Kind: "Issuer",
						},
						Subject: &cmv1.X509Subject{
							Organizations: []string{"ArtemisCloud"},
						},
					},
				}
				Expect(k8sClient.Create(ctx, &cert)).Should(Succeed())

				By("Creating ActiveMQArtemis: " + activeMQArtemis.Name)
				activeMQArtemis.Spec.Acceptors = []brokerv1beta1.AcceptorType{
					{
						Name:       "tls-acceptor",
						Port:       61617,
						SSLEnabled: true,
						SSLSecret:  certSecretName,
					},
				}
				activeMQArtemis.Spec.Env = []corev1.EnvVar{{Name: "JAVA_ARGS_APPEND", Value: "-Djavax.net.debug=all"}}
				//activeMQArtemis.Spec.BrokerProperties = []string{"acceptorConfigurations.tls-acceptor.params.trustStorePath=-"}

				Expect(k8sClient.Create(ctx, &activeMQArtemis)).Should(Succeed())

				podName := activeMQArtemis.Name + "-ss-0"
				// /home/jboss/amq-broker/bin/artemis check node --up --url 'tcp://aa-con-cer-man162a-ss-0:61617?sslEnabled=true&verifyHost=false&trustStoreType=PEMCFG&trustStorePath=/etc/secret-aa-con-cer-man162a-cert-secret-pemcfg/aa-con-cer-man162a-cert-secret.pemcfg'
				command := []string{"/home/jboss/amq-broker/bin/artemis", "check", "node", "--up", "--url", "tcp://" + podName + ":61617?sslEnabled=true&verifyHost=false&trustStoreType=PEMCFG&trustStorePath=/etc/secret-" + activeMQArtemis.Name + "-cert-secret-pemcfg/" + activeMQArtemis.Name + "-cert-secret.pemcfg"}
				Eventually(func(g Gomega) {
					stdOutContent := ExecOnPod(podName, activeMQArtemis.Name, defaultNamespace, command, g)
					g.Expect(stdOutContent).Should(ContainSubstring("Checks run: 1"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				CleanResource(&activeMQArtemis, activeMQArtemis.Name, defaultNamespace)
				CleanResource(&issuer, issuer.Name, defaultNamespace)
				CleanResource(&cert, cert.Name, defaultNamespace)

				certSecret := &corev1.Secret{}
				//By default, cert-manager does not delete the Secret resource containing the signed certificate when the corresponding Certificate resource is deleted.
				if k8sClient.Get(ctx, types.NamespacedName{Name: certSecretName, Namespace: defaultNamespace}, certSecret) == nil {
					CleanResource(certSecret, certSecretName, defaultNamespace)
				}
			})
		})
	})
})

func getConnectorConfig(podName string, crName string, connectorName string, g Gomega) map[string]string {
	curlUrl := "http://" + podName + ":8161/console/jolokia/read/org.apache.activemq.artemis:broker=\"amq-broker\"/ConnectorsAsJSON"
	command := []string{"curl", "-k", "-s", "-u", "testuser:testpassword", curlUrl}

	result := ExecOnPod(podName, crName, defaultNamespace, command, g)

	var rootMap map[string]any
	g.Expect(json.Unmarshal([]byte(result), &rootMap)).To(Succeed())

	rootMapValue := rootMap["value"]
	g.Expect(rootMapValue).ShouldNot(BeNil())
	connectors := rootMapValue.(string)

	var listOfConnectors []ConnectorConfig
	g.Expect(json.Unmarshal([]byte(connectors), &listOfConnectors))

	for _, v := range listOfConnectors {
		if v.Name == connectorName {
			return v.Params
		}
	}
	return nil
}

func checkReadPodStatus(podName string, crName string, g Gomega) {
	curlUrl := "https://" + podName + ":8161/console/jolokia/read/org.apache.activemq.artemis:broker=\"amq-broker\"/Status"
	command := []string{"curl", "-k", "-s", "-u", "testuser:testpassword", curlUrl}

	result := ExecOnPod(podName, crName, defaultNamespace, command, g)
	var rootMap map[string]any
	g.Expect(json.Unmarshal([]byte(result), &rootMap)).To(Succeed())
	value := rootMap["value"].(string)
	var valueMap map[string]any
	g.Expect(json.Unmarshal([]byte(value), &valueMap)).To(Succeed())
	serverInfo := valueMap["server"].(map[string]any)
	serverState := serverInfo["state"].(string)
	g.Expect(serverState).To(Equal("STARTED"))
}

func checkMessagingInPod(podName string, crName string, portNumber string, trustStoreLoc string, g Gomega) {
	tcpUrl := "tcp://" + podName + ":" + portNumber + "?sslEnabled=true&trustStorePath=" + trustStoreLoc + "&trustStoreType=PEM"
	sendCommand := []string{"amq-broker/bin/artemis", "producer", "--user", "testuser", "--password", "testpassword", "--url", tcpUrl, "--message-count", "1", "--destination", "queue://DLQ", "--verbose"}
	result := ExecOnPod(podName, crName, defaultNamespace, sendCommand, g)
	g.Expect(result).To(ContainSubstring("Produced: 1 messages"))
	receiveCommand := []string{"amq-broker/bin/artemis", "consumer", "--user", "testuser", "--password", "testpassword", "--url", tcpUrl, "--message-count", "1", "--destination", "queue://DLQ", "--verbose"}
	result = ExecOnPod(podName, crName, defaultNamespace, receiveCommand, g)
	g.Expect(result).To(ContainSubstring("Consumed: 1 messages"))
}

func testConfiguredWithCertAndBundle(certSecret string, caSecret string) {
	// it should use PEM store type
	By("Deploying the broker cr")
	brokerCrName := brokerCrNameBase + "0"
	brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {

		candidate.Name = brokerCrName

		candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
		candidate.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
			InitialDelaySeconds: 1,
			PeriodSeconds:       1,
			TimeoutSeconds:      5,
		}
		candidate.Spec.Console.Expose = true
		candidate.Spec.Console.SSLEnabled = true
		candidate.Spec.Console.UseClientAuth = false
		candidate.Spec.Console.SSLSecret = certSecret
		candidate.Spec.Console.TrustSecret = &caSecret
	})
	pod0Name := createdBrokerCr.Name + "-ss-0"
	By("Checking the broker status reflect the truth")
	Eventually(func(g Gomega) {
		crdRef := types.NamespacedName{
			Namespace: brokerCr.Namespace,
			Name:      brokerCr.Name,
		}
		g.Expect(k8sClient.Get(ctx, crdRef, createdBrokerCr)).Should(Succeed())

		condition := meta.FindStatusCondition(createdBrokerCr.Status.Conditions, brokerv1beta1.DeployedConditionType)
		g.Expect(condition).NotTo(BeNil())
		g.Expect(condition.Status).Should(Equal(metav1.ConditionTrue))
		checkReadPodStatus(pod0Name, createdBrokerCr.Name, g)
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	CleanResource(createdBrokerCr, brokerCr.Name, createdBrokerCr.Namespace)

	By("Deploying the broker cr exposing acceptor ssl and connector ssl")
	brokerCrName = brokerCrNameBase + "1"
	pod0Name = brokerCrName + "-ss-0"
	brokerCr, createdBrokerCr = DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {

		candidate.Name = brokerCrName
		candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
		candidate.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
			InitialDelaySeconds: 1,
			PeriodSeconds:       1,
			TimeoutSeconds:      5,
		}
		candidate.Spec.Acceptors = []brokerv1beta1.AcceptorType{{
			Name:        "new-acceptor",
			Port:        62666,
			Protocols:   "all",
			Expose:      true,
			SSLEnabled:  true,
			SSLSecret:   certSecret,
			TrustSecret: &caSecret,
		}}
		candidate.Spec.Connectors = []brokerv1beta1.ConnectorType{{
			Name:        "new-connector",
			Host:        pod0Name,
			Port:        62666,
			Expose:      true,
			SSLEnabled:  true,
			SSLSecret:   certSecret,
			TrustSecret: &caSecret,
		}}
	})

	crdRef := types.NamespacedName{
		Namespace: brokerCr.Namespace,
		Name:      brokerCr.Name,
	}

	By("checking the broker status reflect the truth")

	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, crdRef, createdBrokerCr)).Should(Succeed())

		condition := meta.FindStatusCondition(createdBrokerCr.Status.Conditions, brokerv1beta1.DeployedConditionType)
		g.Expect(condition).NotTo(BeNil())
		g.Expect(condition.Status).Should(Equal(metav1.ConditionTrue))
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	By("checking the broker message send and receive")
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, crdRef, createdBrokerCr)).Should(Succeed())
		checkMessagingInPod(pod0Name, createdBrokerCr.Name, "62666", "/etc/"+caBundleName+"-volume/"+caPemTrustStoreName, g)
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	By("checking connector parameters")
	Eventually(func(g Gomega) {
		connectorCfg := getConnectorConfig(pod0Name, createdBrokerCr.Name, "new-connector", g)
		g.Expect(connectorCfg).NotTo(BeNil())
		g.Expect(connectorCfg["keyStoreType"]).To(Equal("PEMCFG"))
		g.Expect(connectorCfg["port"]).To(Equal("62666"))
		g.Expect(connectorCfg["sslEnabled"]).To(Equal("true"))
		g.Expect(connectorCfg["host"]).To(Equal(pod0Name))
		g.Expect(connectorCfg["trustStorePath"]).To(Equal("/etc/" + caBundleName + "-volume/" + caPemTrustStoreName))
		g.Expect(connectorCfg["trustStoreType"]).To(Equal("PEM"))
		g.Expect(connectorCfg["keyStorePath"]).To(Equal("/etc/secret-server-cert-secret-pemcfg/" + certSecret + ".pemcfg"))
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	CleanResource(createdBrokerCr, brokerCr.Name, createdBrokerCr.Namespace)
}
