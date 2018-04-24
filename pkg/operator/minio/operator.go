/*
Copyright 2018 The Rook Authors. All rights reserved.

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

// Package operator to manage Minio object storage.
package minio

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/coreos/pkg/capnslog"
	opkit "github.com/rook/operator-kit"
	"github.com/rook/rook/pkg/clusterd"
	"github.com/rook/rook/pkg/daemon/agent/flexvolume/attachment"
	"github.com/rook/rook/pkg/operator/agent"
	"github.com/rook/rook/pkg/operator/k8sutil"
	"github.com/rook/rook/pkg/operator/minio/object"
	"k8s.io/api/core/v1"
)

var logger = capnslog.NewPackageLogger("github.com/rook/rook", "operator")

// Operator type for managing object storage.
type Operator struct {
	context    *clusterd.Context
	rookImage  string
	controller MinioController
}

// New creates an operator instance
func New(context *clusterd.Context, volumeAttachmentWrapper attachment.Attachment, rookImage string) *Operator {
	clusterController := cluster.NewClusterController(context, rookImage, volumeAttachmentWrapper)
	volumeProvisioner := provisioner.New(context)

	schemes := []opkit.CustomResource{cluster.ClusterResource, pool.PoolResource, object.ObjectStoreResource,
		file.FilesystemResource, attachment.VolumeAttachmentResource}
	return &Operator{
		context:           context,
		clusterController: clusterController,
		resources:         schemes,
		volumeProvisioner: volumeProvisioner,
		rookImage:         rookImage,
	}
}

// Run the operator instance
func (o *Operator) Run() error {

	namespace := os.Getenv(k8sutil.PodNamespaceEnvVar)
	if namespace == "" {
		return fmt.Errorf("Rook operator namespace is not provided. Expose it via downward API in the rook operator manifest file using environment variable %s", k8sutil.PodNamespaceEnvVar)
	}

	rookAgent := agent.New(o.context.Clientset)

	if err := rookAgent.Start(namespace, o.rookImage); err != nil {
		return fmt.Errorf("Error starting agent daemonset: %v", err)
	}

	signalChan := make(chan os.Signal, 1)
	stopChan := make(chan struct{})
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	// Run volume provisioner
	// The controller needs to know what the server version is because out-of-tree
	// provisioners aren't officially supported until 1.5
	serverVersion, err := o.context.Clientset.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("Error getting server version: %v", err)
	}
	pc := controller.NewProvisionController(
		o.context.Clientset,
		provisionerName,
		o.volumeProvisioner,
		serverVersion.GitVersion,
	)
	go pc.Run(stopChan)
	logger.Infof("rook-provisioner started")

	// watch for changes to the rook clusters
	o.clusterController.StartWatch(v1.NamespaceAll, stopChan)

	for {
		select {
		case <-signalChan:
			logger.Infof("shutdown signal received, exiting...")
			close(stopChan)
			return nil
		}
	}
}
