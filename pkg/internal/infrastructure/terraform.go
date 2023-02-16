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

package infrastructure

import (
	"bytes"
	"context"
	"fmt"
	"strconv"

	"github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/terraformer"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	api "github.com/gardener/gardener-extension-provider-openstack/pkg/apis/openstack"
	"github.com/gardener/gardener-extension-provider-openstack/pkg/apis/openstack/helper"
	apiv1alpha1 "github.com/gardener/gardener-extension-provider-openstack/pkg/apis/openstack/v1alpha1"
)

const (
	// TerraformerPurpose is a constant for the complete Terraform setup with purpose 'infrastructure'.
	TerraformerPurpose = "infra"
	// TerraformOutputKeySSHKeyName key for accessing SSH key name from outputs in terraform
	TerraformOutputKeySSHKeyName = "key_name"
	// TerraformOutputKeyRouterID is the id the router between provider network and the worker subnet.
	TerraformOutputKeyRouterID = "router_id"
	// TerraformOutputKeyRouterIP is the ip address of the router.
	TerraformOutputKeyRouterIP = "router_ip"
	// TerraformOutputKeyNetworkID is the private worker network.
	TerraformOutputKeyNetworkID = "network_id"
	// TerraformOutputKeyNetworkName is the private worker network name.
	TerraformOutputKeyNetworkName = "network_name"
	// TerraformOutputKeySecurityGroupID is the id of worker security group.
	TerraformOutputKeySecurityGroupID = "security_group_id"
	// TerraformOutputKeySecurityGroupName is the name of the worker security group.
	TerraformOutputKeySecurityGroupName = "security_group_name"
	// TerraformOutputKeyFloatingNetworkID is the id of the provider network.
	TerraformOutputKeyFloatingNetworkID = "floating_network_id"
	// TerraformOutputKeySubnetID is the id of the worker subnet.
	TerraformOutputKeySubnetID = "subnet_id"
	// TerraformOutputKeyShareNetworkID is the share network.
	TerraformOutputKeyShareNetworkID = "share_network_id"
	// TerraformOutputKeyShareNetworkName is the share network name.
	TerraformOutputKeyShareNetworkName = "share_network_name"

	// DefaultRouterID is the computed router ID as generated by terraform.
	DefaultRouterID = "openstack_networking_router_v2.router.id"
	// MaxApiCallRetries sets the maximum retries for failed requests.
	MaxApiCallRetries = "10"
)

// StatusTypeMeta is the TypeMeta of the GCP InfrastructureStatus
var StatusTypeMeta = metav1.TypeMeta{
	APIVersion: apiv1alpha1.SchemeGroupVersion.String(),
	Kind:       "InfrastructureStatus",
}

// ComputeTerraformerTemplateValues computes the values for the OpenStack Terraformer chart.
func ComputeTerraformerTemplateValues(
	infra *extensionsv1alpha1.Infrastructure,
	config *api.InfrastructureConfig,
	cluster *controller.Cluster,
) (map[string]interface{}, error) {
	var (
		createRouter  = true
		createNetwork = true
		useCACert     = false
		routerConfig  = map[string]interface{}{
			"id": DefaultRouterID,
		}
		outputKeysConfig = map[string]interface{}{
			"routerID":          TerraformOutputKeyRouterID,
			"routerIP":          TerraformOutputKeyRouterIP,
			"networkID":         TerraformOutputKeyNetworkID,
			"networkName":       TerraformOutputKeyNetworkName,
			"keyName":           TerraformOutputKeySSHKeyName,
			"securityGroupID":   TerraformOutputKeySecurityGroupID,
			"securityGroupName": TerraformOutputKeySecurityGroupName,
			"floatingNetworkID": TerraformOutputKeyFloatingNetworkID,
			"subnetID":          TerraformOutputKeySubnetID,
		}
	)

	cloudProfileConfig, err := helper.CloudProfileConfigFromCluster(cluster)
	if err != nil {
		return nil, err
	}

	if config.Networks.Router != nil {
		createRouter = false
		routerConfig["id"] = strconv.Quote(config.Networks.Router.ID)
	}

	if floatingPoolSubnet := findFloatingSubnet(createRouter, config, cloudProfileConfig, infra.Spec.Region); floatingPoolSubnet != nil {
		routerConfig["floatingPoolSubnet"] = *floatingPoolSubnet
	}

	keyStoneURL, err := helper.FindKeyStoneURL(cloudProfileConfig.KeyStoneURLs, cloudProfileConfig.KeyStoneURL, infra.Spec.Region)
	if err != nil {
		return nil, err
	}
	keyStoneCA := helper.FindKeyStoneCACert(cloudProfileConfig.KeyStoneURLs, cloudProfileConfig.KeyStoneCACert, infra.Spec.Region)
	if keyStoneCA != nil && len(*keyStoneCA) > 0 {
		useCACert = true
	}

	if cloudProfileConfig.UseSNAT != nil {
		routerConfig["enableSNAT"] = *cloudProfileConfig.UseSNAT
	}

	workersCIDR := WorkersCIDR(config)
	networksConfig := map[string]interface{}{
		"workers": workersCIDR,
	}
	if config.Networks.ID != nil {
		createNetwork = false
		networksConfig["id"] = *config.Networks.ID
	}

	createShareNetwork := config.Networks.ShareNetwork != nil && config.Networks.ShareNetwork.Enabled
	if createShareNetwork {
		outputKeysConfig["shareNetworkID"] = TerraformOutputKeyShareNetworkID
		outputKeysConfig["shareNetworkName"] = TerraformOutputKeyShareNetworkName
	}

	return map[string]interface{}{
		"openstack": map[string]interface{}{
			"maxApiCallRetries": MaxApiCallRetries,
			"authURL":           keyStoneURL,
			"region":            infra.Spec.Region,
			"insecure":          cloudProfileConfig.KeyStoneForceInsecure,
			"floatingPoolName":  config.FloatingPoolName,
			"useCACert":         useCACert,
		},
		"create": map[string]interface{}{
			"router":       createRouter,
			"network":      createNetwork,
			"shareNetwork": createShareNetwork,
		},
		"dnsServers":   cloudProfileConfig.DNSServers,
		"sshPublicKey": string(infra.Spec.SSHPublicKey),
		"router":       routerConfig,
		"clusterName":  infra.Namespace,
		"networks":     networksConfig,
		"outputKeys":   outputKeysConfig,
	}, nil
}

func findFloatingSubnet(isRouterRequired bool, config *api.InfrastructureConfig, cloudProfileConfig *api.CloudProfileConfig, region string) *string {
	if !isRouterRequired {
		return nil
	}

	// First: Check if the InfrastructureConfig contain a floating subnet and use it.
	if config.FloatingPoolSubnetName != nil {
		return config.FloatingPoolSubnetName
	}

	// Second: Check if the CloudProfile contains a default floating subnet and use it.
	if floatingPool, err := helper.FindFloatingPool(cloudProfileConfig.Constraints.FloatingPools, config.FloatingPoolName, region, nil); err == nil && floatingPool.DefaultFloatingSubnet != nil {
		return floatingPool.DefaultFloatingSubnet
	}

	return nil
}

// RenderTerraformerTemplate renders the openstack infrastructure templates with the given values.
func RenderTerraformerTemplate(
	infra *extensionsv1alpha1.Infrastructure,
	config *api.InfrastructureConfig,
	cluster *controller.Cluster,
) (*TerraformFiles, error) {
	values, err := ComputeTerraformerTemplateValues(infra, config, cluster)
	if err != nil {
		return nil, err
	}

	var mainTF bytes.Buffer
	if err := mainTemplate.Execute(&mainTF, values); err != nil {
		return nil, fmt.Errorf("could not render Terraform template: %+v", err)
	}

	return &TerraformFiles{
		Main:      mainTF.String(),
		Variables: variablesTF,
		TFVars:    terraformTFVars,
	}, nil
}

// TerraformFiles are the files that have been rendered from the infrastructure chart.
type TerraformFiles struct {
	Main      string
	Variables string
	TFVars    []byte
}

// TerraformState is the Terraform state for an infrastructure.
type TerraformState struct {
	// SSHKeyName key for accessing SSH key name from outputs in terraform
	SSHKeyName string
	// RouterID is the id the router between provider network and the worker subnet.
	RouterID string
	// RouterIP is the ip address of the router.
	RouterIP string
	// NetworkID is the private worker network.
	NetworkID string
	// NetworkName is the private worker network name.
	NetworkName string
	// SubnetID is the id of the worker subnet.
	SubnetID string
	// FloatingNetworkID is the id of the provider network.
	FloatingNetworkID string
	// SecurityGroupID is the id of worker security group.
	SecurityGroupID string
	// SecurityGroupName is the name of the worker security group.
	SecurityGroupName string
	// ShareNetworkID is the optional share network ID.
	ShareNetworkID string
	// ShareNetworkName is the optional share network name.
	ShareNetworkName string
}

// ExtractTerraformState extracts the TerraformState from the given Terraformer.
func ExtractTerraformState(ctx context.Context, tf terraformer.Terraformer, config *api.InfrastructureConfig) (*TerraformState, error) {
	outputKeys := []string{
		TerraformOutputKeySSHKeyName,
		TerraformOutputKeyRouterID,
		TerraformOutputKeyRouterIP,
		TerraformOutputKeyNetworkID,
		TerraformOutputKeyNetworkName,
		TerraformOutputKeySubnetID,
		TerraformOutputKeyFloatingNetworkID,
		TerraformOutputKeySecurityGroupID,
		TerraformOutputKeySecurityGroupName,
	}

	if config.Networks.ShareNetwork != nil && config.Networks.ShareNetwork.Enabled {
		outputKeys = append(outputKeys, TerraformOutputKeyShareNetworkID, TerraformOutputKeyShareNetworkName)
	}

	vars, err := tf.GetStateOutputVariables(ctx, outputKeys...)
	if err != nil {
		return nil, err
	}

	return &TerraformState{
		SSHKeyName:        vars[TerraformOutputKeySSHKeyName],
		RouterID:          vars[TerraformOutputKeyRouterID],
		RouterIP:          vars[TerraformOutputKeyRouterIP],
		NetworkID:         vars[TerraformOutputKeyNetworkID],
		NetworkName:       vars[TerraformOutputKeyNetworkName],
		SubnetID:          vars[TerraformOutputKeySubnetID],
		FloatingNetworkID: vars[TerraformOutputKeyFloatingNetworkID],
		SecurityGroupID:   vars[TerraformOutputKeySecurityGroupID],
		SecurityGroupName: vars[TerraformOutputKeySecurityGroupName],
		ShareNetworkID:    vars[TerraformOutputKeyShareNetworkID],
		ShareNetworkName:  vars[TerraformOutputKeyShareNetworkName],
	}, nil
}

// StatusFromTerraformState computes an InfrastructureStatus from the given
// Terraform variables.
func StatusFromTerraformState(state *TerraformState) *apiv1alpha1.InfrastructureStatus {
	var shareNetworkStatus *apiv1alpha1.ShareNetworkStatus
	if state.ShareNetworkID != "" {
		shareNetworkStatus = &apiv1alpha1.ShareNetworkStatus{
			ID:   state.ShareNetworkID,
			Name: state.ShareNetworkName,
		}
	}
	return &apiv1alpha1.InfrastructureStatus{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiv1alpha1.SchemeGroupVersion.String(),
			Kind:       "InfrastructureStatus",
		},
		Networks: apiv1alpha1.NetworkStatus{
			ID:   state.NetworkID,
			Name: state.NetworkName,
			FloatingPool: apiv1alpha1.FloatingPoolStatus{
				ID: state.FloatingNetworkID,
			},
			Router: apiv1alpha1.RouterStatus{
				ID: state.RouterID,
				IP: state.RouterIP,
			},
			Subnets: []apiv1alpha1.Subnet{
				{
					Purpose: apiv1alpha1.PurposeNodes,
					ID:      state.SubnetID,
				},
			},
			ShareNetwork: shareNetworkStatus,
		},
		SecurityGroups: []apiv1alpha1.SecurityGroup{
			{
				Purpose: apiv1alpha1.PurposeNodes,
				ID:      state.SecurityGroupID,
				Name:    state.SecurityGroupName,
			},
		},
		Node: apiv1alpha1.NodeStatus{
			KeyName: state.SSHKeyName,
		},
	}
}

// ComputeStatus computes the status based on the Terraformer and the given InfrastructureConfig.
func ComputeStatus(ctx context.Context, tf terraformer.Terraformer, config *api.InfrastructureConfig) (*apiv1alpha1.InfrastructureStatus, error) {
	state, err := ExtractTerraformState(ctx, tf, config)
	if err != nil {
		return nil, err
	}

	status := StatusFromTerraformState(state)
	status.Networks.FloatingPool.Name = config.FloatingPoolName
	return status, nil
}
