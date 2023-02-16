// Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controlplane

import (
	"context"
	"encoding/json"
	"time"

	calicov1alpha1 "github.com/gardener/gardener-extension-networking-calico/pkg/apis/calico/v1alpha1"
	"github.com/gardener/gardener-extension-networking-calico/pkg/calico"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/controlplane/genericactuator"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	mockclient "github.com/gardener/gardener/pkg/mock/controller-runtime/client"
	"github.com/gardener/gardener/pkg/utils"
	secretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager"
	fakesecretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager/fake"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"

	api "github.com/gardener/gardener-extension-provider-openstack/pkg/apis/openstack"
	openstackv1alpha1 "github.com/gardener/gardener-extension-provider-openstack/pkg/apis/openstack/v1alpha1"
	"github.com/gardener/gardener-extension-provider-openstack/pkg/openstack"
)

const (
	namespace                        = "test"
	authURL                          = "someurl"
	region                           = "europe"
	technicalID                      = "shoot--dev--test"
	genericTokenKubeconfigSecretName = "generic-token-kubeconfig-92e9ae14"
)

var (
	dhcpDomain     = pointer.String("dhcp-domain")
	requestTimeout = &metav1.Duration{
		Duration: func() time.Duration { d, _ := time.ParseDuration("5m"); return d }(),
	}
)

func defaultControlPlane() *extensionsv1alpha1.ControlPlane {
	return defaultControlPlaneWithManila(false)
}

func defaultControlPlaneWithManila(csiManila bool) *extensionsv1alpha1.ControlPlane {
	cpConfig := &api.ControlPlaneConfig{
		LoadBalancerProvider: "load-balancer-provider",
		CloudControllerManager: &api.CloudControllerManagerConfig{
			FeatureGates: map[string]bool{
				"CustomResourceValidation": true,
			},
		},
	}
	var status *api.ShareNetworkStatus
	if csiManila {
		cpConfig.CSI = &api.CSI{Manila: &api.Manila{Enabled: true}}
		status = &api.ShareNetworkStatus{
			ID:   "1111-2222-3333-4444",
			Name: "sharenetwork",
		}
	}
	cp := controlPlane("floating-network-id", cpConfig, status)
	return cp
}

func controlPlane(floatingPoolID string, cfg *api.ControlPlaneConfig, status *api.ShareNetworkStatus) *extensionsv1alpha1.ControlPlane {
	return &extensionsv1alpha1.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "control-plane",
			Namespace: namespace,
		},
		Spec: extensionsv1alpha1.ControlPlaneSpec{
			SecretRef: corev1.SecretReference{
				Name:      v1beta1constants.SecretNameCloudProvider,
				Namespace: namespace,
			},
			DefaultSpec: extensionsv1alpha1.DefaultSpec{
				ProviderConfig: &runtime.RawExtension{
					Raw: encode(cfg),
				},
			},
			Region: region,
			InfrastructureProviderStatus: &runtime.RawExtension{
				Raw: encode(&api.InfrastructureStatus{
					Networks: api.NetworkStatus{
						Name: technicalID,
						FloatingPool: api.FloatingPoolStatus{
							ID: floatingPoolID,
						},
						Router: api.RouterStatus{
							ID: "routerID",
						},
						Subnets: []api.Subnet{
							{
								ID:      "subnet-acbd1234",
								Purpose: api.PurposeNodes,
							},
						},
						ShareNetwork: status,
					},
				}),
			},
		},
	}
}

var _ = Describe("ValuesProvider", func() {
	var (
		ctx = context.TODO()

		ctrl *gomock.Controller

		scheme = runtime.NewScheme()
		_      = api.AddToScheme(scheme)

		fakeClient         client.Client
		fakeSecretsManager secretsmanager.Interface

		vp genericactuator.ValuesProvider
		c  *mockclient.MockClient

		cp = defaultControlPlane()

		cidr                             = "10.250.0.0/19"
		useOctavia                       = true
		rescanBlockStorageOnResize       = true
		ignoreVolumeAZ                   = true
		nodeVoluemAttachLimit      int32 = 25
		technicalID                      = technicalID

		cloudProfileConfig = &api.CloudProfileConfig{
			KeyStoneURL:                authURL,
			DHCPDomain:                 dhcpDomain,
			RequestTimeout:             requestTimeout,
			UseOctavia:                 pointer.Bool(useOctavia),
			RescanBlockStorageOnResize: pointer.Bool(rescanBlockStorageOnResize),
			IgnoreVolumeAZ:             pointer.Bool(ignoreVolumeAZ),
			NodeVolumeAttachLimit:      pointer.Int32(nodeVoluemAttachLimit),
		}
		cloudProfileConfigJSON, _ = json.Marshal(cloudProfileConfig)

		clusterK8sAtLeast120 = &extensionscontroller.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"generic-token-kubeconfig.secret.gardener.cloud/name": genericTokenKubeconfigSecretName,
				},
			},
			CloudProfile: &gardencorev1beta1.CloudProfile{
				Spec: gardencorev1beta1.CloudProfileSpec{
					ProviderConfig: &runtime.RawExtension{
						Raw: cloudProfileConfigJSON,
					},
				},
			},
			Shoot: &gardencorev1beta1.Shoot{
				Spec: gardencorev1beta1.ShootSpec{
					Networking: gardencorev1beta1.Networking{
						Pods: &cidr,
					},
					Kubernetes: gardencorev1beta1.Kubernetes{
						Version: "1.20.4",
						VerticalPodAutoscaler: &gardencorev1beta1.VerticalPodAutoscaler{
							Enabled: true,
						},
					},
					Provider: gardencorev1beta1.Provider{
						InfrastructureConfig: &runtime.RawExtension{
							Raw: encode(&openstackv1alpha1.InfrastructureConfig{
								TypeMeta: metav1.TypeMeta{
									APIVersion: openstackv1alpha1.SchemeGroupVersion.String(),
									Kind:       "InfrastructureConfig",
								},
								Networks: openstackv1alpha1.Networks{
									Workers: "10.200.0.0/19",
								},
							}),
						},
						Workers: []gardencorev1beta1.Worker{
							{
								Name:  "worker",
								Zones: []string{"zone2", "zone1"},
							},
						},
					},
				},
				Status: gardencorev1beta1.ShootStatus{
					TechnicalID: technicalID,
				},
			},
		}
		clusterNoOverlay = &extensionscontroller.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"generic-token-kubeconfig.secret.gardener.cloud/name": genericTokenKubeconfigSecretName,
				},
			},
			CloudProfile: &gardencorev1beta1.CloudProfile{
				Spec: gardencorev1beta1.CloudProfileSpec{
					ProviderConfig: &runtime.RawExtension{
						Raw: cloudProfileConfigJSON,
					},
				},
			},
			Shoot: &gardencorev1beta1.Shoot{
				Spec: gardencorev1beta1.ShootSpec{
					Networking: gardencorev1beta1.Networking{
						Type: calico.ReleaseName,
						ProviderConfig: &runtime.RawExtension{
							Object: &calicov1alpha1.NetworkConfig{
								TypeMeta: metav1.TypeMeta{},
								Overlay: &calicov1alpha1.Overlay{
									Enabled: false,
								},
							},
						},
						Pods: &cidr,
					},
					Kubernetes: gardencorev1beta1.Kubernetes{
						Version: "1.25.4",
						VerticalPodAutoscaler: &gardencorev1beta1.VerticalPodAutoscaler{
							Enabled: true,
						},
					},
				},
				Status: gardencorev1beta1.ShootStatus{
					TechnicalID: technicalID,
				},
			},
		}

		domainName  = "domain-name"
		tenantName  = "tenant-name"
		cpSecretKey = client.ObjectKey{Namespace: namespace, Name: v1beta1constants.SecretNameCloudProvider}
		cpSecret    = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      v1beta1constants.SecretNameCloudProvider,
				Namespace: namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"domainName": []byte(domainName),
				"tenantName": []byte(tenantName),
				"username":   []byte(`username`),
				"password":   []byte(`password`),
				"authURL":    []byte(authURL),
			},
		}

		cpConfigKey = client.ObjectKey{Namespace: namespace, Name: openstack.CloudProviderConfigName}
		cpConfig    = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      openstack.CloudProviderConfigName,
				Namespace: namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				openstack.CloudProviderConfigDataKey: []byte("some data"),
			},
		}

		cloudProviderDiskConfig = []byte("foo")
		cpCSIDiskConfigKey      = client.ObjectKey{Namespace: namespace, Name: openstack.CloudProviderCSIDiskConfigName}
		cpCSIDiskConfig         = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      openstack.CloudProviderCSIDiskConfigName,
				Namespace: namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				openstack.CloudProviderConfigDataKey: cloudProviderDiskConfig,
			},
		}

		checksums = map[string]string{
			v1beta1constants.SecretNameCloudProvider: "8bafb35ff1ac60275d62e1cbd495aceb511fb354f74a20f7d06ecb48b3a68432",
			openstack.CloudProviderConfigName:        "bf19236c3ff3be18cf28cb4f58532bda4fd944857dd163baa05d23f952550392",
			openstack.CloudProviderCSIDiskConfigName: "77627eb2343b9f2dc2fca3cce35f2f9eec55783aa5f7dac21c473019e5825de2",
		}

		enabledTrue  = map[string]interface{}{"enabled": true}
		enabledFalse = map[string]interface{}{"enabled": false}
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())

		c = mockclient.NewMockClient(ctrl)

		fakeClient = fakeclient.NewClientBuilder().Build()
		fakeSecretsManager = fakesecretsmanager.New(fakeClient, namespace)

		vp = NewValuesProvider()
		err := vp.(inject.Scheme).InjectScheme(scheme)
		Expect(err).NotTo(HaveOccurred())
		err = vp.(inject.Client).InjectClient(c)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Describe("#GetConfigChartValues", func() {
		configChartValues := map[string]interface{}{
			"domainName":                  "domain-name",
			"tenantName":                  "tenant-name",
			"username":                    "username",
			"password":                    "password",
			"region":                      region,
			"subnetID":                    "subnet-acbd1234",
			"lbProvider":                  "load-balancer-provider",
			"floatingNetworkID":           "floating-network-id",
			"insecure":                    false,
			"authUrl":                     authURL,
			"dhcpDomain":                  dhcpDomain,
			"requestTimeout":              requestTimeout,
			"useOctavia":                  useOctavia,
			"rescanBlockStorageOnResize":  rescanBlockStorageOnResize,
			"ignoreVolumeAZ":              ignoreVolumeAZ,
			"nodeVolumeAttachLimit":       pointer.Int32(nodeVoluemAttachLimit),
			"applicationCredentialID":     "",
			"applicationCredentialSecret": "",
			"applicationCredentialName":   "",
			"internalNetworkName":         technicalID,
		}

		It("should return correct config chart values", func() {
			c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret))

			values, err := vp.GetConfigChartValues(ctx, cp, clusterK8sAtLeast120)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(Equal(configChartValues))
		})

		It("should return correct config chart values with load balancer classes", func() {
			c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret))

			var (
				floatingNetworkID  = "4711"
				fsid               = "0815"
				floatingSubnetID2  = "pub0815"
				floatingSubnetName = "*public*"
				floatingSubnetTags = "tag1,tag2"
				subnetID           = "priv"
				floatingSubnetID   = "default-floating-subnet-id"
				cp                 = controlPlane(
					floatingNetworkID,
					&api.ControlPlaneConfig{
						LoadBalancerProvider: "load-balancer-provider",
						LoadBalancerClasses: []api.LoadBalancerClass{
							{
								Name:              "test",
								FloatingNetworkID: &floatingNetworkID,
								FloatingSubnetID:  &fsid,
								SubnetID:          nil,
							},
							{
								Name:             "default",
								FloatingSubnetID: &floatingSubnetID,
								SubnetID:         nil,
							},
							{
								Name:             "public",
								FloatingSubnetID: &floatingSubnetID2,
								SubnetID:         nil,
							},
							{
								Name:               "fip-subnet-by-name",
								FloatingSubnetName: &floatingSubnetName,
							},
							{
								Name:               "fip-subnet-by-tags",
								FloatingSubnetTags: &floatingSubnetTags,
							},
							{
								Name:     "other",
								SubnetID: &subnetID,
							},
						},
						CloudControllerManager: &api.CloudControllerManagerConfig{
							FeatureGates: map[string]bool{
								"CustomResourceValidation": true,
							},
						},
					},
					nil,
				)

				expectedValues = utils.MergeMaps(configChartValues, map[string]interface{}{
					"floatingNetworkID": floatingNetworkID,
					"floatingSubnetID":  floatingSubnetID,
					"floatingClasses": []map[string]interface{}{
						{
							"name":              "test",
							"floatingNetworkID": floatingNetworkID,
							"floatingSubnetID":  fsid,
						},
						{
							"name":             "default",
							"floatingSubnetID": floatingSubnetID,
						},
						{
							"name":             "public",
							"floatingSubnetID": floatingSubnetID2,
						},
						{
							"name":               "fip-subnet-by-name",
							"floatingSubnetName": floatingSubnetName,
						},
						{
							"name":               "fip-subnet-by-tags",
							"floatingSubnetTags": floatingSubnetTags,
						},
						{
							"name":     "other",
							"subnetID": subnetID,
						},
					},
				})
			)

			values, err := vp.GetConfigChartValues(ctx, cp, clusterK8sAtLeast120)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(Equal(expectedValues))
		})

		It("should return correct config chart values with load balancer classes with purpose", func() {
			c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret))

			var (
				floatingNetworkID = "fip1"
				cp                = controlPlane(
					floatingNetworkID,
					&api.ControlPlaneConfig{
						LoadBalancerProvider: "load-balancer-provider",
						LoadBalancerClasses: []api.LoadBalancerClass{
							{
								Name:             "default",
								FloatingSubnetID: pointer.String("fip-subnet-1"),
							},
							{
								Name:             "real-default",
								FloatingSubnetID: pointer.String("fip-subnet-2"),
								Purpose:          pointer.String("default"),
							},
						},
						CloudControllerManager: &api.CloudControllerManagerConfig{
							FeatureGates: map[string]bool{
								"CustomResourceValidation": true,
							},
						},
					},
					nil,
				)

				expectedValues = utils.MergeMaps(configChartValues, map[string]interface{}{
					"floatingNetworkID": floatingNetworkID,
					"floatingSubnetID":  "fip-subnet-2",
					"floatingClasses": []map[string]interface{}{
						{
							"name":             "default",
							"floatingSubnetID": "fip-subnet-1",
						},
						{
							"name":             "real-default",
							"floatingSubnetID": "fip-subnet-2",
						},
					},
				})
			)

			values, err := vp.GetConfigChartValues(ctx, cp, clusterK8sAtLeast120)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(Equal(expectedValues))
		})

		It("should return correct config chart values with application credentials", func() {
			secret2 := *cpSecret
			secret2.Data = map[string][]byte{
				"domainName":                  []byte(domainName),
				"tenantName":                  []byte(tenantName),
				"applicationCredentialID":     []byte(`app-id`),
				"applicationCredentialSecret": []byte(`app-secret`),
				"authURL":                     []byte(authURL),
			}

			c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(&secret2))

			expectedValues := utils.MergeMaps(configChartValues, map[string]interface{}{
				"username":                    "",
				"password":                    "",
				"applicationCredentialID":     "app-id",
				"applicationCredentialSecret": "app-secret",
				"insecure":                    false,
			})
			values, err := vp.GetConfigChartValues(ctx, cp, clusterK8sAtLeast120)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(Equal(expectedValues))
		})

		It("should configure cloud routes when not using overlay", func() {
			c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret))
			expectedValues := utils.MergeMaps(configChartValues, map[string]interface{}{
				"routerID": "routerID",
			})
			values, err := vp.GetConfigChartValues(ctx, cp, clusterNoOverlay)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(Equal(expectedValues))
		})

		It("should return correct config chart values with KeyStone CA Cert", func() {
			secret2 := cpSecret.DeepCopy()
			caCert := "custom-cert"
			secret2.Data["caCert"] = []byte(caCert)
			c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(secret2))
			expectedValues := utils.MergeMaps(configChartValues, map[string]interface{}{
				"caCert": caCert,
			})
			values, err := vp.GetConfigChartValues(ctx, cp, clusterK8sAtLeast120)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(Equal(expectedValues))
		})
	})

	Describe("#GetControlPlaneChartValues", func() {
		ccmChartValues := utils.MergeMaps(enabledTrue, map[string]interface{}{
			"replicas":          1,
			"kubernetesVersion": "1.20.1",
			"clusterName":       namespace,
			"podNetwork":        cidr,
			"podAnnotations": map[string]interface{}{
				"checksum/secret-" + v1beta1constants.SecretNameCloudProvider: checksums[v1beta1constants.SecretNameCloudProvider],
				"checksum/secret-" + openstack.CloudProviderConfigName:        checksums[openstack.CloudProviderConfigName],
			},
			"podLabels": map[string]interface{}{
				"maintenance.gardener.cloud/restart": "true",
			},
			"featureGates": map[string]bool{
				"CustomResourceValidation": true,
			},
			"tlsCipherSuites": []string{
				"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
				"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305",
				"TLS_RSA_WITH_AES_128_CBC_SHA",
				"TLS_RSA_WITH_AES_256_CBC_SHA",
				"TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA",
			},
			"secrets": map[string]interface{}{
				"server": "cloud-controller-manager-server",
			},
		})

		BeforeEach(func() {
			c.EXPECT().Get(ctx, cpConfigKey, &corev1.Secret{}).DoAndReturn(clientGet(cpConfig))
			c.EXPECT().Delete(context.TODO(), &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "allow-kube-apiserver-to-csi-snapshot-validation", Namespace: cp.Namespace}})

			By("creating secrets managed outside of this package for whose secretsmanager.Get() will be called")
			Expect(fakeClient.Create(context.TODO(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ca-provider-openstack-controlplane", Namespace: namespace}})).To(Succeed())
			Expect(fakeClient.Create(context.TODO(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "csi-snapshot-validation-server", Namespace: namespace}})).To(Succeed())
			Expect(fakeClient.Create(context.TODO(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "cloud-controller-manager-server", Namespace: namespace}})).To(Succeed())
		})

		It("should return correct control plane chart values", func() {
			c.EXPECT().Get(ctx, cpCSIDiskConfigKey, &corev1.Secret{}).DoAndReturn(clientGet(cpCSIDiskConfig))
			c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret))

			values, err := vp.GetControlPlaneChartValues(ctx, cp, clusterK8sAtLeast120, fakeSecretsManager, checksums, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(Equal(map[string]interface{}{
				"global": map[string]interface{}{
					"genericTokenKubeconfigSecretName": genericTokenKubeconfigSecretName,
				},
				openstack.CloudControllerManagerName: utils.MergeMaps(ccmChartValues, map[string]interface{}{
					"userAgentHeaders":  []string{domainName, tenantName, technicalID},
					"kubernetesVersion": clusterK8sAtLeast120.Shoot.Spec.Kubernetes.Version,
				}),
				openstack.CSIControllerName: utils.MergeMaps(enabledTrue, map[string]interface{}{
					"replicas": 1,
					"podAnnotations": map[string]interface{}{
						"checksum/secret-" + openstack.CloudProviderCSIDiskConfigName: checksums[openstack.CloudProviderCSIDiskConfigName],
					},
					"userAgentHeaders": []string{domainName, tenantName, technicalID},
					"csiSnapshotController": map[string]interface{}{
						"replicas": 1,
					},
					"csiSnapshotValidationWebhook": map[string]interface{}{
						"replicas": 1,
						"secrets": map[string]interface{}{
							"server": "csi-snapshot-validation-server",
						},
					},
				}),
			}))
		})
	})

	Describe("#GetControlPlaneShootChartValues", func() {
		BeforeEach(func() {
			By("creating secrets managed outside of this package for whose secretsmanager.Get() will be called")
			Expect(fakeClient.Create(context.TODO(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ca-provider-openstack-controlplane", Namespace: namespace}})).To(Succeed())
			Expect(fakeClient.Create(context.TODO(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "csi-snapshot-validation-server", Namespace: namespace}})).To(Succeed())
			Expect(fakeClient.Create(context.TODO(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "cloud-controller-manager-server", Namespace: namespace}})).To(Succeed())
		})

		Context("shoot control plane chart values", func() {
			It("should return correct shoot control plane chart when ca is secret found", func() {
				c.EXPECT().Get(ctx, cpCSIDiskConfigKey, &corev1.Secret{}).DoAndReturn(clientGet(cpCSIDiskConfig))
				c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret))

				values, err := vp.GetControlPlaneShootChartValues(ctx, cp, clusterK8sAtLeast120, fakeSecretsManager, map[string]string{})
				Expect(err).NotTo(HaveOccurred())
				Expect(values).To(Equal(map[string]interface{}{
					openstack.CloudControllerManagerName: enabledTrue,
					openstack.CSINodeName: utils.MergeMaps(enabledTrue, map[string]interface{}{
						"vpaEnabled": true,
						"podAnnotations": map[string]interface{}{
							"checksum/secret-" + openstack.CloudProviderCSIDiskConfigName: checksums[openstack.CloudProviderCSIDiskConfigName],
						},
						"userAgentHeaders":    []string{domainName, tenantName, technicalID},
						"cloudProviderConfig": cloudProviderDiskConfig,
						"webhookConfig": map[string]interface{}{
							"url":      "https://csi-snapshot-validation.test/volumesnapshot",
							"caBundle": "",
						},
						"pspDisabled": false,
					}),
					openstack.CSIDriverManila: enabledFalse,
					openstack.CSIDriverNFS:    enabledFalse,
				}))
			})

			It("should return correct shoot control plane chart if CSI Manila is enabled", func() {
				c.EXPECT().Get(ctx, cpCSIDiskConfigKey, &corev1.Secret{}).DoAndReturn(clientGet(cpCSIDiskConfig))
				c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret))

				cpManila := defaultControlPlaneWithManila(true)
				values, err := vp.GetControlPlaneShootChartValues(ctx, cpManila, clusterK8sAtLeast120, fakeSecretsManager, map[string]string{})
				Expect(err).NotTo(HaveOccurred())
				Expect(values).To(Equal(map[string]interface{}{
					openstack.CloudControllerManagerName: enabledTrue,
					openstack.CSINodeName: utils.MergeMaps(enabledTrue, map[string]interface{}{
						"vpaEnabled": true,
						"podAnnotations": map[string]interface{}{
							"checksum/secret-" + openstack.CloudProviderCSIDiskConfigName: checksums[openstack.CloudProviderCSIDiskConfigName],
						},
						"userAgentHeaders":    []string{domainName, tenantName, technicalID},
						"cloudProviderConfig": cloudProviderDiskConfig,
						"webhookConfig": map[string]interface{}{
							"url":      "https://csi-snapshot-validation.test/volumesnapshot",
							"caBundle": "",
						},
						"pspDisabled": false,
					}),
					openstack.CSIDriverManila: utils.MergeMaps(enabledTrue, map[string]interface{}{
						"csimanila": map[string]interface{}{
							"clusterID": "test",
						},
						"openstack": map[string]interface{}{
							"projectName":                 "tenant-name",
							"userName":                    "username",
							"password":                    "password",
							"applicationCredentialID":     "",
							"applicationCredentialName":   "",
							"availabilityZones":           []string{"zone1", "zone2"},
							"authURL":                     authURL,
							"region":                      "europe",
							"applicationCredentialSecret": "",
							"shareClient":                 "10.200.0.0/19",
							"shareNetworkID":              "1111-2222-3333-4444",
							"domainName":                  "domain-name",
							"tlsInsecure":                 "",
							"caCert":                      "",
						},
						"pspDisabled": false,
						"vpaEnabled":  true,
					}),
					openstack.CSIDriverNFS: utils.MergeMaps(enabledTrue, map[string]interface{}{
						"pspDisabled": false,
						"vpaEnabled":  true,
					}),
				}))
			})
		})

		Context("PodSecurityPolicy", func() {
			It("should return correct shoot control plane chart when PodSecurityPolicy admission plugin is not disabled in the shoot", func() {
				clusterK8sAtLeast120.Shoot.Spec.Kubernetes.KubeAPIServer = &gardencorev1beta1.KubeAPIServerConfig{
					AdmissionPlugins: []gardencorev1beta1.AdmissionPlugin{
						{
							Name: "PodSecurityPolicy",
						},
					},
				}
				c.EXPECT().Get(ctx, cpCSIDiskConfigKey, &corev1.Secret{}).DoAndReturn(clientGet(cpCSIDiskConfig))
				c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret))

				values, err := vp.GetControlPlaneShootChartValues(ctx, cp, clusterK8sAtLeast120, fakeSecretsManager, map[string]string{})
				Expect(err).NotTo(HaveOccurred())
				Expect(values).To(Equal(map[string]interface{}{
					openstack.CloudControllerManagerName: enabledTrue,
					openstack.CSINodeName: utils.MergeMaps(enabledTrue, map[string]interface{}{
						"vpaEnabled": true,
						"podAnnotations": map[string]interface{}{
							"checksum/secret-" + openstack.CloudProviderCSIDiskConfigName: checksums[openstack.CloudProviderCSIDiskConfigName],
						},
						"userAgentHeaders":    []string{domainName, tenantName, technicalID},
						"cloudProviderConfig": cloudProviderDiskConfig,
						"webhookConfig": map[string]interface{}{
							"url":      "https://csi-snapshot-validation.test/volumesnapshot",
							"caBundle": "",
						},
						"pspDisabled": false,
					}),
					openstack.CSIDriverManila: enabledFalse,
					openstack.CSIDriverNFS:    enabledFalse,
				}))
			})
			It("should return correct shoot control plane chart when PodSecurityPolicy admission plugin is disabled in the shoot", func() {
				clusterK8sAtLeast120.Shoot.Spec.Kubernetes.KubeAPIServer = &gardencorev1beta1.KubeAPIServerConfig{
					AdmissionPlugins: []gardencorev1beta1.AdmissionPlugin{
						{
							Name:     "PodSecurityPolicy",
							Disabled: pointer.Bool(true),
						},
					},
				}
				c.EXPECT().Get(ctx, cpCSIDiskConfigKey, &corev1.Secret{}).DoAndReturn(clientGet(cpCSIDiskConfig))
				c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret))

				values, err := vp.GetControlPlaneShootChartValues(ctx, cp, clusterK8sAtLeast120, fakeSecretsManager, map[string]string{})
				Expect(err).NotTo(HaveOccurred())
				Expect(values).To(Equal(map[string]interface{}{
					openstack.CloudControllerManagerName: enabledTrue,
					openstack.CSINodeName: utils.MergeMaps(enabledTrue, map[string]interface{}{
						"vpaEnabled": true,
						"podAnnotations": map[string]interface{}{
							"checksum/secret-" + openstack.CloudProviderCSIDiskConfigName: checksums[openstack.CloudProviderCSIDiskConfigName],
						},
						"userAgentHeaders":    []string{domainName, tenantName, technicalID},
						"cloudProviderConfig": cloudProviderDiskConfig,
						"webhookConfig": map[string]interface{}{
							"url":      "https://csi-snapshot-validation.test/volumesnapshot",
							"caBundle": "",
						},
						"pspDisabled": true,
					}),
					openstack.CSIDriverManila: enabledFalse,
					openstack.CSIDriverNFS:    enabledFalse,
				}))
			})
		})
	})

	Describe("#GetStorageClassesChartValues", func() {
		It("should return correct storage class chart values", func() {
			values, err := vp.GetStorageClassesChartValues(ctx, cp, clusterK8sAtLeast120)
			Expect(err).NotTo(HaveOccurred())
			Expect(values["storageclasses"]).To(HaveLen(2))
			Expect(values["storageclasses"].([]map[string]interface{})[0]["provisioner"]).To(Equal(openstack.CSIStorageProvisioner))
			Expect(values["storageclasses"].([]map[string]interface{})[1]["provisioner"]).To(Equal(openstack.CSIStorageProvisioner))
		})
	})
})

func encode(obj runtime.Object) []byte {
	data, _ := json.Marshal(obj)
	return data
}

func clientGet(result runtime.Object) interface{} {
	return func(ctx context.Context, key client.ObjectKey, obj runtime.Object, _ ...client.GetOption) error {
		switch obj.(type) {
		case *corev1.Secret:
			*obj.(*corev1.Secret) = *result.(*corev1.Secret)
		}
		return nil
	}
}
