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

// Package to manage a Minio object store.
package minio

import (
	"reflect"

	"github.com/coreos/pkg/capnslog"
	opkit "github.com/rook/operator-kit"
	miniov1alpha1 "github.com/rook/rook/pkg/apis/minio.rook.io/v1alpha1"
	"github.com/rook/rook/pkg/clusterd"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	customResourceName       = "objectstore"
	customResourceNamePlural = "objectstores"
)

var logger = capnslog.NewPackageLogger("github.com/rook/rook", "minio-op-object")

// ObjectStoreResource represents the object store custom resource
var ObjectStoreResource = opkit.CustomResource{
	Name:    customResourceName,
	Plural:  customResourceNamePlural,
	Group:   miniov1alpha1.CustomResourceGroup,
	Version: miniov1alpha1.Version,
	Scope:   apiextensionsv1beta1.NamespaceScoped,
	Kind:    reflect.TypeOf(miniov1alpha1.ObjectStore{}).Name(),
}

// ObjectStoreController represents a controller object for object store custom resources
type ObjectStoreController struct {
	context     *clusterd.Context
	rookImage   string
	hostNetwork bool
	ownerRef    metav1.OwnerReference
}

// NewObjectStoreController create controller for watching object store custom resources created
func NewObjectStoreController(context *clusterd.Context, rookImage string, hostNetwork bool, ownerRef metav1.OwnerReference) *ObjectStoreController {
	return &ObjectStoreController{
		context:     context,
		rookImage:   rookImage,
		hostNetwork: hostNetwork,
		ownerRef:    ownerRef,
	}
}

// StartWatch watches for instances of ObjectStore custom resources and acts on them
func (c *ObjectStoreController) StartWatch(namespace string, stopCh chan struct{}) error {
	resourceHandlerFuncs := cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onAdd,
		UpdateFunc: c.onUpdate,
		DeleteFunc: c.onDelete,
	}

	logger.Infof("start watching object store resources in namespace %s", namespace)
	watcher := opkit.NewWatcher(ObjectStoreResource, namespace, resourceHandlerFuncs, c.context.RookClientset.MinioV1alpha1().RESTClient())
	go watcher.Watch(&miniov1alpha1.ObjectStore{}, stopCh)

	return nil
}

func (c *ObjectStoreController) onAdd(obj interface{}) {
	objectstore, err := c.getObjectStoreObject(obj)
	if err != nil {
		logger.Errorf("failed to get objectstore object: %+v", err)
		return
	}

	// TODO: Do stuff.
}

func (c *ObjectStoreController) onUpdate(oldObj, newObj interface{}) {
	oldStore, err := c.getObjectStoreObject(oldObj)
	if err != nil {
		logger.Errorf("failed to get old objectstore object: %+v", err)
		return
	}

	newStore, err := c.getObjectStoreObject(newObj)
	if err != nil {
		logger.Errorf("failed to get new objectstore object: %+v", err)
		return
	}

	// TODO: Do stuff.
}

func (c *ObjectStoreController) onDelete(obj interface{}) {
	objectstore, err := c.getObjectStoreObject(obj)
	if err != nil {
		logger.Errorf("failed to get objectstore object: %+v", err)
		return
	}

	// This should fail.
	onDelete(obj, what) // TODO: Do stuff
}

func (c *ObjectStoreController) getObjectStoreObject(obj interface{}) (objectstore *miniov1alpha1.ObjectStore, err error) {
	return objectstore.DeepCopy(), nil
}