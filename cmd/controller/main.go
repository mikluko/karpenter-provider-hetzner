// Copyright The karpenter-provider-hetzner Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"sigs.k8s.io/controller-runtime/pkg/log"

	// Register karpenter core types into the default k8s scheme.
	_ "sigs.k8s.io/karpenter/pkg/apis/v1"

	"sigs.k8s.io/karpenter/pkg/cloudprovider/overlay"
	"sigs.k8s.io/karpenter/pkg/controllers"
	"sigs.k8s.io/karpenter/pkg/controllers/node/health"
	"sigs.k8s.io/karpenter/pkg/controllers/state"
	"sigs.k8s.io/karpenter/pkg/operator"

	// Register our HCloudNodeClass v1 types.
	_ "sarak.sh/mikluko/karpenter-provider-hetzner/pkg/apis/v1"

	"sarak.sh/mikluko/karpenter-provider-hetzner/pkg/bootstrap"
	hetznercp "sarak.sh/mikluko/karpenter-provider-hetzner/pkg/cloudprovider"
	nodeclasshash "sarak.sh/mikluko/karpenter-provider-hetzner/pkg/controllers/nodeclaim/hash"
	"sarak.sh/mikluko/karpenter-provider-hetzner/pkg/controllers/nodeclass"
	hetznerop "sarak.sh/mikluko/karpenter-provider-hetzner/pkg/operator"
	"sarak.sh/mikluko/karpenter-provider-hetzner/pkg/providers/imagefamily"
	"sarak.sh/mikluko/karpenter-provider-hetzner/pkg/providers/instance"
	"sarak.sh/mikluko/karpenter-provider-hetzner/pkg/providers/instancetype"
)

func main() {
	ctx, op := operator.NewOperator()

	cfg, err := hetznerop.LoadConfig()
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to load config")
		return
	}

	// Create the Hetzner Cloud API client. Built from cfg so the API timeout is
	// applied to the HTTP client the SDK is constructed with; it cannot be set
	// afterwards.
	hcloudClient, err := hetznerop.NewHCloudClient(cfg)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to create Hetzner Cloud client")
		return
	}

	// Create the three providers.
	instanceProvider := instance.NewProviderWithPlacementGroups(&hcloudClient.Server, &hcloudClient.PlacementGroup, cfg.ClusterName, &hcloudClient.Action)
	typeProvider := instancetype.NewProvider(&hcloudClient.ServerType)
	imageProvider := imagefamily.NewProvider(&hcloudClient.Image)

	clusterCA, err := hetznerop.ClusterCA(op.GetConfig())
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to load the cluster CA")
		return
	}
	joiner := bootstrap.NewJoiner(op.GetClient(), cfg.APIServerURL, clusterCA)

	// Create the cloud provider.
	baseCloudProvider := hetznercp.NewCloudProvider(
		op.GetClient(),
		instanceProvider,
		typeProvider,
		imageProvider,
		joiner,
	)

	// Wrap with the overlay decorator (required by NewControllers).
	cloudProvider := overlay.Decorate(baseCloudProvider, op.GetClient(), op.InstanceTypeStore)

	// nil unless the NodeRepair feature gate is on; then node repair and
	// cluster state share it.
	repairPolicyMatcher, err := health.NewRepairPolicyMatcher(ctx, cloudProvider)
	if err != nil {
		log.FromContext(ctx).Error(err, "invalid repair policies")
		return
	}

	// Create cluster state.
	clusterState := state.NewCluster(op.Clock, op.GetClient(), cloudProvider, state.WithRepairPolicyMatcher(repairPolicyMatcher))

	// Our NodeClass status controller (network + image validation, Ready).
	nodeClassController := nodeclass.NewController(op.GetClient(), &hcloudClient.Network, &hcloudClient.Location, &hcloudClient.Firewall, &hcloudClient.SSHKey, imageProvider)

	// Wire and start all controllers.
	op.WithControllers(ctx, append(
		controllers.NewControllers(
			ctx,
			op.Manager,
			op.Clock,
			op.GetClient(),
			op.EventRecorder,
			cloudProvider,
			baseCloudProvider,
			clusterState,
			op.InstanceTypeStore,
			op.PredictionStore,
		),
		nodeClassController,
		nodeclasshash.NewController(op.GetClient()),
	)...).Start(ctx)
}
