package upgrade

import (
	"fmt"

	"github.com/rancher/apiserver/pkg/types"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/rancher/harvester/pkg/generated/controllers/harvester.cattle.io/v1alpha1"
)

const (
	stateUpgrading    = "Upgrading"
	upgradeStateLabel = "harvester.cattle.io/upgradeState"
)

// store block upgrade creation if there's any ongoing upgrade
type store struct {
	namespace string
	types.Store
	upgradeCache v1alpha1.UpgradeCache
}

func (s *store) Create(request *types.APIRequest, schema *types.APISchema, data types.APIObject) (types.APIObject, error) {
	sets := labels.Set{
		upgradeStateLabel: stateUpgrading,
	}
	upgrades, err := s.upgradeCache.List(s.namespace, sets.AsSelector())
	if err != nil {
		return data, err
	}
	if len(upgrades) > 0 {
		return data, fmt.Errorf("cannot proceed until previous upgrade %q completes", upgrades[0].Name)
	}
	return s.Store.Create(request, schema, data)
}
