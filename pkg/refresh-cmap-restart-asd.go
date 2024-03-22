package pkg

import (
	goctx "context"
	"fmt"
)

// QuickRestart refreshes Aerospike config map and tries to warm restart Aerospike.
func (initp *InitParams) QuickRestart(ctx goctx.Context, cmName, cmNamespace string) error {
	if cmNamespace == "" {
		return fmt.Errorf("kubernetes namespace required as an argument")
	}

	if cmName == "" {
		return fmt.Errorf("aerospike configmap required as an argument")
	}

	if err := initp.ExportK8sConfigmap(ctx, cmNamespace, cmName, configMapDir); err != nil {
		return err
	}

	if err := initp.restartASD(); err != nil {
		return err
	}

	// Update pod status in the k8s aerospike cluster object
	return initp.manageVolumesAndUpdateStatus(ctx, "quickRestart")
}

func (initp *InitParams) UpdateConf(ctx goctx.Context, cmName, cmNamespace string) error {
	if cmNamespace == "" {
		return fmt.Errorf("kubernetes namespace required as an argument")
	}

	if cmName == "" {
		return fmt.Errorf("aerospike configmap required as an argument")
	}

	if err := initp.ExportK8sConfigmap(ctx, cmNamespace, cmName, configMapDir); err != nil {
		return err
	}

	// Create new Aerospike configuration
	if err := initp.copyTemplates(configMapDir, configVolume); err != nil {
		return err
	}

	if err := initp.createAerospikeConf(); err != nil {
		return err
	}

	// Update pod status in the k8s aerospike cluster object
	return initp.manageVolumesAndUpdateStatus(ctx, "noRestart")
}
