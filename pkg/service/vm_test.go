/*
Copyright 2026.

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

package service

import (
	"encoding/json"
	"testing"

	"github.com/onsi/gomega"
	"github.com/smartxworks/cloudtower-go-sdk/v2/models"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
)

func TestCreateVMFromTemplateParams(t *testing.T) {
	t.Run("when storage cluster is set should use top-level storage config", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		params := newTestCreateVMFromTemplateParams(t, infrav1.ElfClusterTypeStandard, &models.StorageConfig{StorageClusterID: TowerString("storage-cluster")})

		g.Expect(params.StorageConfig).NotTo(gomega.BeNil())
		g.Expect(*params.StorageConfig.StorageClusterID).To(gomega.Equal("storage-cluster"))

		serialized, err := json.Marshal(params)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		var payload struct {
			StorageConfig *models.StorageConfig `json:"storage_config,omitempty"`
		}
		g.Expect(json.Unmarshal(serialized, &payload)).To(gomega.Succeed())
		g.Expect(payload.StorageConfig).NotTo(gomega.BeNil())
		g.Expect(*payload.StorageConfig.StorageClusterID).To(gomega.Equal("storage-cluster"))
	})

	t.Run("when datastore is set should use top-level storage config", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		params := newTestCreateVMFromTemplateParams(t, infrav1.ElfClusterTypeStandard, &models.StorageConfig{DatastoreID: TowerString("datastore")})

		g.Expect(params.StorageConfig).NotTo(gomega.BeNil())
		g.Expect(*params.StorageConfig.DatastoreID).To(gomega.Equal("datastore"))
		g.Expect(params.StorageConfig.StorageClusterID).To(gomega.BeNil())
	})

	t.Run("when storage cluster is unset should omit storage config", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		params := newTestCreateVMFromTemplateParams(t, infrav1.ElfClusterTypeStandard, nil)

		g.Expect(params.StorageConfig).To(gomega.BeNil())

		serialized, err := json.Marshal(params)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		var payload map[string]json.RawMessage
		g.Expect(json.Unmarshal(serialized, &payload)).To(gomega.Succeed())
		g.Expect(payload).NotTo(gomega.HaveKey("storage_config"))
	})

	t.Run("when stretched and storage cluster is set should use disk storage config", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		params := newTestCreateVMFromTemplateParams(t, infrav1.ElfClusterTypeStretched, &models.StorageConfig{StorageClusterID: TowerString("storage-cluster")})

		g.Expect(params.DiskOperate).NotTo(gomega.BeNil())
		g.Expect(params.DiskOperate.NewDisks.MountNewCreateDisks).To(gomega.HaveLen(1))
		g.Expect(params.DiskOperate.NewDisks.MountNewCreateDisks[0].StorageConfig).NotTo(gomega.BeNil())
		g.Expect(*params.DiskOperate.NewDisks.MountNewCreateDisks[0].StorageConfig.StorageClusterID).To(gomega.Equal("storage-cluster"))

		serialized, err := json.Marshal(params)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		var payload struct {
			DiskOperate *struct {
				NewDisks *struct {
					MountNewCreateDisks []struct {
						StorageConfig *models.StorageConfig `json:"storage_config,omitempty"`
					} `json:"mount_new_create_disks,omitempty"`
				} `json:"new_disks,omitempty"`
			} `json:"disk_operate,omitempty"`
		}
		g.Expect(json.Unmarshal(serialized, &payload)).To(gomega.Succeed())
		g.Expect(payload.DiskOperate).NotTo(gomega.BeNil())
		g.Expect(payload.DiskOperate.NewDisks.MountNewCreateDisks).To(gomega.HaveLen(1))
		g.Expect(payload.DiskOperate.NewDisks.MountNewCreateDisks[0].StorageConfig).NotTo(gomega.BeNil())
		g.Expect(*payload.DiskOperate.NewDisks.MountNewCreateDisks[0].StorageConfig.StorageClusterID).To(gomega.Equal("storage-cluster"))
	})
}

func newTestCreateVMFromTemplateParams(t *testing.T, clusterType infrav1.ElfClusterType, config *models.StorageConfig) *models.VMCreateVMFromContentLibraryTemplateParams {
	t.Helper()

	service := &TowerVMService{}
	elfCluster := &infrav1.ElfCluster{
		Spec: infrav1.ElfClusterSpec{ClusterType: clusterType},
	}
	elfMachine := &infrav1.ElfMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "machine"},
		Spec: infrav1.ElfMachineSpec{
			NumCPUs:           1,
			NumCoresPerSocket: 1,
			MemoryMiB:         1024,
			OSType:            "LINUX",
		},
	}
	template := &models.ContentLibraryVMTemplate{
		ID: TowerString("template"),
	}
	if clusterType == infrav1.ElfClusterTypeStretched {
		template.VMDisks = []*models.NestedContentLibraryVMTemplateDisk{
			{
				Type:  models.NewVMDiskType(models.VMDiskTypeDISK),
				Boot:  TowerInt32(0),
				Bus:   models.NewBus(models.BusVIRTIO),
				Index: TowerInt32(0),
				Size:  TowerInt64(1024),
			},
		}
	}

	params, err := service.createVMFromTemplateParams(
		elfCluster,
		elfMachine,
		&models.Cluster{ID: TowerString("compute-cluster")},
		template,
		&CloneVMInfo{StorageConfig: config},
	)
	if err != nil {
		t.Fatalf("createVMFromTemplateParams() error = %v", err)
	}

	return params
}
