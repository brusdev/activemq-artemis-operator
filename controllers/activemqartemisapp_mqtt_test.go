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
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"os"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	brokerv1beta1 "github.com/arkmq-org/activemq-artemis-operator/api/v1beta1"
	"github.com/arkmq-org/activemq-artemis-operator/pkg/resources/ingresses"
	"github.com/arkmq-org/activemq-artemis-operator/pkg/resources/secrets"
	"github.com/arkmq-org/activemq-artemis-operator/pkg/utils/common"
)

var _ = Describe("artemis-service", func() {

	var installedCertManager bool = false

	BeforeEach(func() {
		BeforeEachSpec()

		if verbose {
			fmt.Println("Time with MicroSeconds: ", time.Now().Format("2006-01-02 15:04:05.000000"), " test:", CurrentSpecReport())
		}

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
			InstallCaBundle(common.DefaultOperatorCASecretName, rootCertSecretName, caPemTrustStoreName)

			By("installing operator cert")
			InstallCert(common.DefaultOperatorCertSecretName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = common.DefaultOperatorCertSecretName
				candidate.Spec.CommonName = "activemq-artemis-operator"
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

		}

	})

	AfterEach(func() {

		if false && os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			UnInstallCaBundle(common.DefaultOperatorCASecretName)
			UninstallClusteredIssuer(caIssuerName)
			UninstallCert(rootCert.Name, rootCert.Namespace)
			UninstallCert(common.DefaultOperatorCertSecretName, defaultNamespace)
			UninstallClusteredIssuer(rootIssuerName)

			if installedCertManager {
				Expect(UninstallCertManager()).To(Succeed())
				installedCertManager = false
			}
		}
		AfterEachSpec()
	})

	Context("mqtt round trip simple", func() {

		It("non persistent", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()

			serviceName := NextSpecResourceName()

			sharedOperandCertName := serviceName + "-" + common.DefaultOperandCertSecretName
			By("installing broker cert")
			InstallCert(sharedOperandCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = sharedOperandCertName
				candidate.Spec.CommonName = serviceName
				candidate.Spec.DNSNames = []string{serviceName, common.ClusterDNSWildCard(serviceName, defaultNamespace)}
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			jvmRemoteDebug := false
			crd := brokerv1beta1.ActiveMQArtemisService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ActiveMQArtemisService",
					APIVersion: brokerv1beta1.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels:    map[string]string{"forMQTT": "true"},
				},
				Spec: brokerv1beta1.ActiveMQArtemisServiceSpec{

					//Env: []corev1.EnvVar{
					//	{
					//		Name:  "JAVA_ARGS_APPEND",
					//		Value: "-Dlog4j2.level=DEBUG",
					//	},
					//},
					Auth:      []brokerv1beta1.AppAuthType{brokerv1beta1.MTLS},
					Acceptors: []brokerv1beta1.AppAcceptor{{Name: "mqtt"}},
				},
			}

			crd.Spec.Resources = corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			}

			if jvmRemoteDebug {
				crd.Spec.Env = append(crd.Spec.Env,
					corev1.EnvVar{
						Name:  "JDK_JAVA_OPTIONS",
						Value: "-agentlib:jdwp=transport=dt_socket,server=y,suspend=y,address=5005",
					})
			}

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			var debugService *corev1.Service = nil
			if jvmRemoteDebug {
				// minikube> kubectl port-forward svc/debug 5005:5005 --namespace test
				By("setup debug")
				debugService = &corev1.Service{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "Service",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "debug",
						Namespace: defaultNamespace,
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeNodePort,
						Selector: map[string]string{
							"ActiveMQArtemis": crd.Name,
						},
						Ports: []corev1.ServicePort{
							{
								Port: 5005,
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, debugService)).Should(Succeed())
			}

			By("deploying a matching app")
			appName := "mqtt-app"
			app := brokerv1beta1.ActiveMQArtemisApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ActiveMQArtemisApp",
					APIVersion: brokerv1beta1.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      appName,
					Namespace: defaultNamespace,
				},
				Spec: brokerv1beta1.ActiveMQArtemisAppSpec{

					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"forMQTT": "true",
						}},

					Auth: []brokerv1beta1.AppAuthType{
						brokerv1beta1.MTLS,
					},

					Capabilities: []brokerv1beta1.AppCapabilityType{
						{
							// Role: appName, TODO respect role for mtls
							ProducerAndConsumerOf: []brokerv1beta1.AppAddressType{{Name: "mytopic"}},
						},
					},

					// Some Resource requirement, that needs to be satisified by matched service
				},
			}

			appCertName := app.Name + common.AppCertSecretSuffix
			By("installing app client cert")
			InstallCert(appCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = appCertName
				candidate.Spec.CommonName = app.Name
				candidate.Spec.Subject.Organizations = nil
				candidate.Spec.Subject.OrganizationalUnits = []string{defaultNamespace}
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			By("Deploying the App " + app.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &app)).Should(Succeed())

			By("verify app status")
			appKey := types.NamespacedName{Name: app.Name, Namespace: crd.Namespace}
			createdApp := &brokerv1beta1.ActiveMQArtemisApp{}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())

				if verbose {
					fmt.Printf("App STATUS: %v\n\n", createdApp.Status.Conditions)
				}
				g.Expect(meta.IsStatusConditionTrue(createdApp.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			//svc.NewServiceDefinitionForCR(types.NamespacedName{Namespace: defaultNamespace, Name: serviceName + "mqtt"}, k8sClient, "mqtt", DefaultServicePort, map[string]string{"ActiveMQArtemis": crd.Name}, labels, nil)
			acceptorIngressHost := serviceName + "-" + defaultNamespace + "." + defaultTestIngressDomain
			acceptorIngress := ingresses.NewIngressForCRWithSSL(nil, types.NamespacedName{Namespace: defaultNamespace, Name: serviceName}, nil, serviceName, "61616", true, defaultTestIngressDomain, acceptorIngressHost, false)
			Expect(k8sClient.Create(ctx, acceptorIngress)).Should(Succeed())

			sharedOperandCertNameSecret, err := secrets.RetriveSecret(types.NamespacedName{Namespace: defaultNamespace, Name: sharedOperandCertName}, make(map[string]string), k8sClient)
			Expect(err).Should(BeNil())

			certpool := x509.NewCertPool()
			certpool.AppendCertsFromPEM(sharedOperandCertNameSecret.Data["tls.crt"])

			appCertNameSecret, err := secrets.RetriveSecret(types.NamespacedName{Namespace: defaultNamespace, Name: appCertName}, make(map[string]string), k8sClient)
			Expect(err).Should(BeNil())

			clientKeyPair, err := tls.X509KeyPair(appCertNameSecret.Data["tls.crt"], appCertNameSecret.Data["tls.key"])
			Expect(err).Should(BeNil())

			//TODO: change JAAS status check
			// Watch login.config and other files in the secret for updates
			// Refresh the configuration: javax.security.auth.login.Configuration.getConfiguration().refresh();
			// Split JaasPropertiesApplied in JaasPropertiesUpdated and JaasPropertiesApplied
			// Ready condition should be false if JaasPropertiesUpdated is false
			time.Sleep(30 * time.Second)

			tlsConfig := &tls.Config{RootCAs: certpool, Certificates: []tls.Certificate{clientKeyPair}, ServerName: acceptorIngressHost, InsecureSkipVerify: true}

			opts := mqtt.NewClientOptions()
			opts.AddBroker("ssl://" + clusterIngressHost + ":443")
			opts.SetClientID("my-client")
			opts.SetTLSConfig(tlsConfig)

			// Define the onConnect handler
			opts.OnConnect = func(c mqtt.Client) {
				fmt.Println("Successfully connected to the broker!")
			}

			messageReceived := false
			messageHandler := func(client mqtt.Client, msg mqtt.Message) {
				messageReceived = true
				fmt.Printf("Received message: '%s' from topic: %s\n", msg.Payload(), msg.Topic())
			}

			// Create and connect the client
			client := mqtt.NewClient(opts)
			if token := client.Connect(); token.Wait() && token.Error() != nil {
				log.Fatalf("Failed to connect to broker: %v", token.Error())
			}

			//org.apache.activemq.artemis.api.core.ActiveMQSecurityException: AMQ229213: User: mqtt-app-test does not have permission='CREATE_NON_DURABLE_QUEUE' for queue my-client.mytopic on address mytopic

			if token := client.Subscribe("mytopic", 1, messageHandler); token.Wait() && token.Error() != nil {
				log.Fatalf("Failed to subscribe to topic: %v", token.Error())
			}

			text := "Hello MQTT from Go!"
			if token := client.Publish("mytopic", 0, false, text); token.Wait() && token.Error() != nil {
				log.Fatalf("Failed to publish to topic: %v", token.Error())
			}

			Eventually(func(g Gomega) {
				g.Expect(messageReceived).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			// Disconnect
			client.Disconnect(250)

			By("removing acceptor ingress")
			Expect(k8sClient.Delete(ctx, acceptorIngress)).Should(Succeed())

			By("removing app")
			Expect(k8sClient.Delete(ctx, createdApp)).Should(Succeed())

			By("tidy up")
			Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())

			if jvmRemoteDebug {
				Expect(k8sClient.Delete(ctx, debugService)).Should(Succeed())
			}

			UninstallCert(appCertName, defaultNamespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
		})
	})

})
