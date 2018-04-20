/*
Copyright 2016 The Rook Authors. All rights reserved.

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
package pool

import (
	"fmt"
	"testing"

	cephv1alpha1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1alpha1"
	"github.com/rook/rook/pkg/clusterd"
	exectest "github.com/rook/rook/pkg/util/exec/test"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidatePool(t *testing.T) {
	context := &clusterd.Context{Executor: &exectest.MockExecutor{}}

	// must specify some replication or EC settings
	p := cephv1alpha1.Pool{ObjectMeta: metav1.ObjectMeta{Name: "mypool", Namespace: "myns"}}
	err := ValidatePool(context, &p)
	assert.NotNil(t, err)

	// must specify name
	p = cephv1alpha1.Pool{ObjectMeta: metav1.ObjectMeta{Namespace: "myns"}}
	err = ValidatePool(context, &p)
	assert.NotNil(t, err)

	// must specify namespace
	p = cephv1alpha1.Pool{ObjectMeta: metav1.ObjectMeta{Name: "mypool"}}
	err = ValidatePool(context, &p)
	assert.NotNil(t, err)

	// must not specify both replication and EC settings
	p = cephv1alpha1.Pool{ObjectMeta: metav1.ObjectMeta{Name: "mypool", Namespace: "myns"}}
	p.Spec.Replicated.Size = 1
	p.Spec.ErasureCoded.CodingChunks = 2
	p.Spec.ErasureCoded.DataChunks = 3
	err = ValidatePool(context, &p)
	assert.NotNil(t, err)

	// succeed with replication settings
	p = cephv1alpha1.Pool{ObjectMeta: metav1.ObjectMeta{Name: "mypool", Namespace: "myns"}}
	p.Spec.Replicated.Size = 1
	err = ValidatePool(context, &p)
	assert.Nil(t, err)

	// succeed with ec settings
	p = cephv1alpha1.Pool{ObjectMeta: metav1.ObjectMeta{Name: "mypool", Namespace: "myns"}}
	p.Spec.ErasureCoded.CodingChunks = 1
	p.Spec.ErasureCoded.DataChunks = 2
	err = ValidatePool(context, &p)
	assert.Nil(t, err)
}

func TestValidateCrushProperties(t *testing.T) {
	executor := &exectest.MockExecutor{}
	context := &clusterd.Context{Executor: executor}
	executor.MockExecuteCommandWithOutputFile = func(debug bool, actionName, command, outputFile string, args ...string) (string, error) {
		logger.Infof("Command: %s %v", command, args)
		if args[1] == "crush" && args[2] == "dump" {
			return `{"types":[{"type_id": 0,"name": "osd"}],"buckets":[{"id": -1,"name":"default"},{"id": -2,"name":"good"}]}`, nil
		}
		return "", fmt.Errorf("unexpected ceph command '%v'", args)
	}

	// succeed with a failure domain that exists
	p := &cephv1alpha1.Pool{
		ObjectMeta: metav1.ObjectMeta{Name: "mypool", Namespace: "myns"},
		Spec: cephv1alpha1.PoolSpec{
			Replicated:    cephv1alpha1.ReplicatedSpec{Size: 1},
			FailureDomain: "osd",
		},
	}
	err := ValidatePool(context, p)
	assert.Nil(t, err)

	// fail with a failure domain that doesn't exist
	p.Spec.FailureDomain = "doesntexist"
	err = ValidatePool(context, p)
	assert.NotNil(t, err)

	// fail with a crush root that doesn't exist
	p.Spec.FailureDomain = "osd"
	p.Spec.CrushRoot = "bad"
	err = ValidatePool(context, p)
	assert.NotNil(t, err)

	// fail with a crush root that does exist
	p.Spec.CrushRoot = "good"
	err = ValidatePool(context, p)
	assert.Nil(t, err)
}

func TestCreatePool(t *testing.T) {
	executor := &exectest.MockExecutor{
		MockExecuteCommandWithOutputFile: func(debug bool, actionName, command, outfile string, args ...string) (string, error) {
			if command == "ceph" && args[1] == "erasure-code-profile" {
				return `{"k":"2","m":"1","plugin":"jerasure","technique":"reed_sol_van"}`, nil
			}
			return "", nil
		},
	}
	context := &clusterd.Context{Executor: executor}

	p := &cephv1alpha1.Pool{ObjectMeta: metav1.ObjectMeta{Name: "mypool", Namespace: "myns"}}
	p.Spec.Replicated.Size = 1

	exists, err := poolExists(context, p)
	assert.False(t, exists)
	err = createPool(context, p)
	assert.Nil(t, err)

	// fail if both replication and EC are specified
	p.Spec.ErasureCoded.CodingChunks = 2
	p.Spec.ErasureCoded.DataChunks = 2
	err = createPool(context, p)
	assert.NotNil(t, err)

	// succeed with EC
	p.Spec.Replicated.Size = 0
	err = createPool(context, p)
	assert.Nil(t, err)
}

func TestUpdatePool(t *testing.T) {
	// the pool did not change for properties that are updatable
	old := cephv1alpha1.PoolSpec{FailureDomain: "osd", ErasureCoded: cephv1alpha1.ErasureCodedSpec{CodingChunks: 2, DataChunks: 2}}
	new := cephv1alpha1.PoolSpec{FailureDomain: "host", ErasureCoded: cephv1alpha1.ErasureCodedSpec{CodingChunks: 3, DataChunks: 3}}
	changed := poolChanged(old, new)
	assert.False(t, changed)

	// the pool changed for properties that are updatable
	old = cephv1alpha1.PoolSpec{FailureDomain: "osd", Replicated: cephv1alpha1.ReplicatedSpec{Size: 1}}
	new = cephv1alpha1.PoolSpec{FailureDomain: "osd", Replicated: cephv1alpha1.ReplicatedSpec{Size: 2}}
	changed = poolChanged(old, new)
	assert.True(t, changed)
}

func TestDeletePool(t *testing.T) {
	executor := &exectest.MockExecutor{
		MockExecuteCommandWithOutputFile: func(debug bool, actionName, command, outfile string, args ...string) (string, error) {
			if command == "ceph" && args[1] == "lspools" {
				return `[{"poolnum":1,"poolname":"mypool"}]`, nil
			} else if command == "ceph" && args[1] == "pool" && args[2] == "get" {
				return `{"pool": "mypool","pool_id": 1,"size":1}`, nil
			}
			return "", nil
		},
	}
	context := &clusterd.Context{Executor: executor}

	// delete a pool that exists
	p := &cephv1alpha1.Pool{ObjectMeta: metav1.ObjectMeta{Name: "mypool", Namespace: "myns"}}
	exists, err := poolExists(context, p)
	assert.Nil(t, err)
	assert.True(t, exists)
	err = deletePool(context, p)
	assert.Nil(t, err)

	// succeed even if the pool doesn't exist
	p = &cephv1alpha1.Pool{ObjectMeta: metav1.ObjectMeta{Name: "otherpool", Namespace: "myns"}}
	exists, err = poolExists(context, p)
	assert.Nil(t, err)
	assert.False(t, exists)
	err = deletePool(context, p)
	assert.Nil(t, err)
}
