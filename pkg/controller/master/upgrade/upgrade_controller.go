package upgrade

import (
	v1 "github.com/rancher/wrangler-api/pkg/generated/controllers/batch/v1"
	ctlcorev1 "github.com/rancher/wrangler-api/pkg/generated/controllers/core/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"

	apisv1alpha1 "github.com/rancher/harvester/pkg/apis/harvester.cattle.io/v1alpha1"
	"github.com/rancher/harvester/pkg/generated/controllers/harvester.cattle.io/v1alpha1"
	upgradectlv1 "github.com/rancher/harvester/pkg/generated/controllers/upgrade.cattle.io/v1"
	"github.com/rancher/harvester/pkg/settings"
)

const (
	//system upgrade controller is deployed in k3os-system namespace
	k3osSystemNamespace            = "k3os-system"
	k3osUpgradeServiceAccount      = "k3os-upgrade"
	kubeSystemNamespace            = "kube-system"
	harvesterSystemNamespace       = "harvester-system"
	harvesterVersionLabel          = "harvester.cattle.io/version"
	harvesterUpgradeLabel          = "harvester.cattle.io/upgrade"
	harvesterManagedLabel          = "harvester.cattle.io/managed"
	harvesterLatestUpgradeLabel    = "harvester.cattle.io/latestUpgrade"
	harvesterUpgradeComponentLabel = "harvester.cattle.io/upgradeComponent"
	upgradeImageRepository         = "rancher/harvester-bundle"
)

// upgradeHandler Creates Plan CRDs to trigger upgrades
type upgradeHandler struct {
	namespace     string
	nodeCache     ctlcorev1.NodeCache
	jobClient     v1.JobClient
	upgradeClient v1alpha1.UpgradeClient
	upgradeCache  v1alpha1.UpgradeCache
	planClient    upgradectlv1.PlanClient
}

func (h *upgradeHandler) OnChanged(key string, upgrade *apisv1alpha1.Upgrade) (*apisv1alpha1.Upgrade, error) {
	if upgrade == nil || upgrade.DeletionTimestamp != nil {
		return upgrade, nil
	}

	if apisv1alpha1.UpgradeCompleted.GetStatus(upgrade) == "" {
		if err := h.resetLatestUpgradeLabel(upgrade.Name); err != nil {
			return upgrade, err
		}

		disableEviction, err := h.isSingleNodeCluster()
		if err != nil {
			return upgrade, err
		}

		// create plans if not initialized
		toUpdate := upgrade.DeepCopy()
		if _, err := h.planClient.Create(serverPlan(upgrade, disableEviction)); err != nil && !apierrors.IsAlreadyExists(err) {
			setNodesUpgradedCondition(toUpdate, corev1.ConditionFalse, "", err.Error())
			return h.upgradeClient.Update(toUpdate)
		}
		initStatus(toUpdate)
		return h.upgradeClient.Update(toUpdate)
	}

	if apisv1alpha1.NodesUpgraded.IsTrue(upgrade) && apisv1alpha1.SystemServicesUpgraded.GetStatus(upgrade) == "" {
		//nodes are upgraded, now upgrade the chart. Create a job to apply the manifests
		toUpdate := upgrade.DeepCopy()
		if _, err := h.jobClient.Create(applyManifestsJob(upgrade)); err != nil && !apierrors.IsAlreadyExists(err) {
			setHelmChartUpgradeStatus(toUpdate, corev1.ConditionFalse, "", err.Error())
			return h.upgradeClient.Update(toUpdate)
		}
		setHelmChartUpgradeStatus(toUpdate, corev1.ConditionUnknown, "", "")
		return h.upgradeClient.Update(toUpdate)
	}

	return upgrade, nil
}

func (h *upgradeHandler) isSingleNodeCluster() (bool, error) {
	nodes, err := h.nodeCache.List(labels.Everything())
	if err != nil {
		return false, err
	}
	return len(nodes) == 1, nil
}

func initStatus(upgrade *apisv1alpha1.Upgrade) {
	apisv1alpha1.UpgradeCompleted.CreateUnknownIfNotExists(upgrade)
	apisv1alpha1.NodesUpgraded.CreateUnknownIfNotExists(upgrade)
	if upgrade.Labels == nil {
		upgrade.Labels = make(map[string]string)
	}
	upgrade.Labels[upgradeStateLabel] = stateUpgrading
	upgrade.Labels[harvesterLatestUpgradeLabel] = "true"
	upgrade.Status.PreviousVersion = settings.ServerVersion.Get()
}

func (h *upgradeHandler) resetLatestUpgradeLabel(latestUpgradeName string) error {
	sets := labels.Set{
		harvesterLatestUpgradeLabel: "true",
	}
	upgrades, err := h.upgradeCache.List(h.namespace, sets.AsSelector())
	if err != nil {
		return err
	}
	for _, upgrade := range upgrades {
		if upgrade.Name == latestUpgradeName {
			continue
		}
		toUpdate := upgrade.DeepCopy()
		delete(toUpdate.Labels, harvesterLatestUpgradeLabel)
		if _, err := h.upgradeClient.Update(toUpdate); err != nil {
			return err
		}
	}
	return nil
}
