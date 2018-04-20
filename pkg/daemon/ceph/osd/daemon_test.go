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
package osd

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	cephv1alpha1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1alpha1"
	"github.com/rook/rook/pkg/clusterd"
	"github.com/rook/rook/pkg/operator/ceph/cluster/osd/config"
	exectest "github.com/rook/rook/pkg/util/exec/test"
	"github.com/stretchr/testify/assert"
)

func TestRunDaemon(t *testing.T) {
	configDir, _ := ioutil.TempDir("", "")
	defer os.RemoveAll(configDir)
	os.MkdirAll(configDir, 0755)

	agent, _, context := createTestAgent(t, "none", configDir, "node5375", &cephv1alpha1.StoreConfig{StoreType: config.Bluestore})
	agent.usingDeviceFilter = true

	done := make(chan struct{})
	go func() {
		done <- struct{}{}
	}()

	err := Run(context, agent, done)
	assert.Nil(t, err)
}

func TestGetDataDirs(t *testing.T) {
	configDir, _ := ioutil.TempDir("", "")
	defer os.RemoveAll(configDir)
	context := &clusterd.Context{ConfigDir: configDir}
	defer os.RemoveAll(context.ConfigDir)
	os.MkdirAll(context.ConfigDir, 0755)

	kv := mockKVStore()
	nodeName := "node6046"

	// user has specified devices to use, no dirs should be returned
	dirMap, removedDirMap, err := getDataDirs(context, kv, "", true, nodeName)
	assert.Nil(t, err)
	assert.Equal(t, 0, len(dirMap))
	assert.Equal(t, 0, len(removedDirMap))

	// user has no devices specified, should return default dir
	dirMap, removedDirMap, err = getDataDirs(context, kv, "", false, nodeName)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(dirMap))
	assert.Equal(t, unassignedOSDID, dirMap[context.ConfigDir])
	assert.Equal(t, 0, len(removedDirMap))

	// user has no devices specified but does specify dirs, those should be returned
	dirMap, removedDirMap, err = getDataDirs(context, kv, "/rook/dir1", false, nodeName)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(dirMap))
	assert.Equal(t, unassignedOSDID, dirMap["/rook/dir1"])
	assert.Equal(t, 0, len(removedDirMap))

	// user has devices specified and also specifies dirs, those should be returned
	dirMap, removedDirMap, err = getDataDirs(context, kv, "/rook/dir1", true, nodeName)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(dirMap))
	assert.Equal(t, unassignedOSDID, dirMap["/rook/dir1"])
	assert.Equal(t, 0, len(removedDirMap))

	// simulate an OSD ID being assigned to the dir
	dirMap["/rook/dir1"] = 1
	// save the directory config
	err = config.SaveOSDDirMap(kv, nodeName, dirMap)
	assert.Nil(t, err)

	// user has specified devices and also a new directory to use.  it should be added to the dir map
	dirMap, removedDirMap, err = getDataDirs(context, kv, "/rook/dir1,/tmp/mydir", true, nodeName)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(dirMap))
	assert.Equal(t, 1, dirMap["/rook/dir1"])
	assert.Equal(t, unassignedOSDID, dirMap["/tmp/mydir"])
	assert.Equal(t, 0, len(removedDirMap))

	// simulate that the user's dir got an OSD by assigning it an ID
	dirMap["/tmp/mydir"] = 23
	err = config.SaveOSDDirMap(kv, nodeName, dirMap)
	assert.Nil(t, err)

	// user is still specifying the 2 directories, we should get back their IDs
	dirMap, removedDirMap, err = getDataDirs(context, kv, "/rook/dir1,/tmp/mydir", true, nodeName)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(dirMap))
	assert.Equal(t, 1, dirMap["/rook/dir1"])
	assert.Equal(t, 23, dirMap["/tmp/mydir"])
	assert.Equal(t, 0, len(removedDirMap))

	// user is now only specifying 1 of the dirs, the other 1 should be returned as removed
	dirMap, removedDirMap, err = getDataDirs(context, kv, "/rook/dir1", true, nodeName)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(dirMap))
	assert.Equal(t, 1, dirMap["/rook/dir1"])
	assert.Equal(t, 1, len(removedDirMap))
	assert.Equal(t, 23, removedDirMap["/tmp/mydir"])

	// clear the dir map and simulate the scenario where an OSD has been created in the default dir
	kv.ClearStore(config.GetConfigStoreName(nodeName))
	osdID := 9802
	dirMap = map[string]int{context.ConfigDir: osdID}
	err = config.SaveOSDDirMap(kv, nodeName, dirMap)
	assert.Nil(t, err)

	// when an OSD has been created in the default dir, no dirs are specified, and no devices are specified,
	// the default dir should still be in use (it should not come back as removed!)
	dirMap, removedDirMap, err = getDataDirs(context, kv, "", false, nodeName)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(dirMap))
	assert.Equal(t, osdID, dirMap[context.ConfigDir])
	assert.Equal(t, 0, len(removedDirMap))

	// if devices are specified (but no dirs), the existing osd in the default dir will not be preserved
	dirMap, removedDirMap, err = getDataDirs(context, kv, "", true, nodeName)
	assert.Nil(t, err)
	assert.Equal(t, 0, len(dirMap))
	assert.Equal(t, 1, len(removedDirMap))
	assert.Equal(t, osdID, removedDirMap[context.ConfigDir])
}

func TestAvailableDevices(t *testing.T) {
	executor := &exectest.MockExecutor{}
	// set up a mock function to return "rook owned" partitions on the device and it does not have a filesystem
	executor.MockExecuteCommandWithOutput = func(debug bool, name string, command string, args ...string) (string, error) {
		logger.Infof("OUTPUT for %s. %s %+v", name, command, args)

		if command == "lsblk" {
			if strings.Index(name, "sdb") != -1 {
				// /dev/sdb has a partition
				return `NAME="sdb" SIZE="65" TYPE="disk" PKNAME=""
NAME="sdb1" SIZE="30" TYPE="part" PKNAME="sdb"`, nil
			}
			return "", nil
		} else if command == "blkid" {
			if strings.Index(name, "sdb1") != -1 {
				// partition sdb1 has a label MY-PART
				return "MY-PART", nil
			}
		} else if command == "df" {
			if strings.Index(name, "sdc") != -1 {
				// /dev/sdc has a file system
				return "/dev/sdc ext4", nil
			}
			return "", nil
		}

		return "", fmt.Errorf("unknown command %s %+v", command, args)
	}

	context := &clusterd.Context{Executor: executor}
	context.Devices = []*clusterd.LocalDisk{
		{Name: "sda"},
		{Name: "sdb"},
		{Name: "sdc"},
		{Name: "sdd"},
		{Name: "nvme01"},
		{Name: "rda"},
		{Name: "rdb"},
	}

	// select all devices, including nvme01 for metadata
	mapping, err := getAvailableDevices(context, "all", "nvme01", true)
	assert.Nil(t, err)
	assert.Equal(t, 5, len(mapping.Entries))
	assert.Equal(t, -1, mapping.Entries["sda"].Data)
	assert.Equal(t, -1, mapping.Entries["sdd"].Data)
	assert.Equal(t, -1, mapping.Entries["rda"].Data)
	assert.Equal(t, -1, mapping.Entries["rdb"].Data)
	assert.Equal(t, -1, mapping.Entries["nvme01"].Data)
	assert.NotNil(t, mapping.Entries["nvme01"].Metadata)
	assert.Equal(t, 0, len(mapping.Entries["nvme01"].Metadata))

	// select no devices both using and not using a filter
	mapping, err = getAvailableDevices(context, "", "", false)
	assert.Nil(t, err)
	assert.Equal(t, 0, len(mapping.Entries))

	mapping, err = getAvailableDevices(context, "", "", true)
	assert.Nil(t, err)
	assert.Equal(t, 0, len(mapping.Entries))

	// select the sd* devices
	mapping, err = getAvailableDevices(context, "^sd.$", "", true)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(mapping.Entries))
	assert.Equal(t, -1, mapping.Entries["sda"].Data)
	assert.Equal(t, -1, mapping.Entries["sdd"].Data)

	// select an exact device
	mapping, err = getAvailableDevices(context, "sdd", "", false)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(mapping.Entries))
	assert.Equal(t, -1, mapping.Entries["sdd"].Data)

	// select all devices except those that have a prefix of "s"
	mapping, err = getAvailableDevices(context, "^[^s]", "", true)
	assert.Nil(t, err)
	assert.Equal(t, 3, len(mapping.Entries))
	assert.Equal(t, -1, mapping.Entries["rda"].Data)
	assert.Equal(t, -1, mapping.Entries["rdb"].Data)
	assert.Equal(t, -1, mapping.Entries["nvme01"].Data)
}

func TestGetRemovedDevices(t *testing.T) {
	testGetRemovedDevicesHelper(t, &cephv1alpha1.StoreConfig{StoreType: config.Bluestore})
	testGetRemovedDevicesHelper(t, &cephv1alpha1.StoreConfig{StoreType: config.Filestore})
}

func testGetRemovedDevicesHelper(t *testing.T, storeConfig *cephv1alpha1.StoreConfig) {
	configDir, _ := ioutil.TempDir("", "")
	defer os.RemoveAll(configDir)
	os.MkdirAll(configDir, 0755)
	nodeName := "node3391"
	agent, _, _ := createTestAgent(t, "none", configDir, nodeName, storeConfig)

	// mock the pre-existence of osd 1 on device sdx
	_, _, _ = mockPartitionSchemeEntry(t, 1, "sdx", &agent.storeConfig, agent.kv, nodeName)

	// get the removed devices for this configuration (note we said to use devices "none" above),
	// it should be osd 1 on device sdx
	scheme, mapping, err := getRemovedDevices(agent)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(mapping.Entries))
	assert.Equal(t, 1, len(scheme.Entries))

	// assert the scheme has an entry for osd 1 and its data partition is on sdx
	schemeEntry := scheme.Entries[0]
	assert.Equal(t, 1, schemeEntry.ID)
	assert.Equal(t, "sdx", schemeEntry.Partitions[schemeEntry.GetDataPartitionType()].Device)

	// assert the removed device mapping has an entry for device sdx and it points to osd 1
	mappingEntry, ok := mapping.Entries["sdx"]
	assert.True(t, ok)
	assert.NotNil(t, mappingEntry)
	assert.Equal(t, 1, mappingEntry.Data)
}
