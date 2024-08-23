/*
Copyright 2024.
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

package controllers

import (
	"bytes"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
)

var _ = Describe("ElfMachineTemplateReconciler", func() {
	var (
		elfCluster *infrav1.ElfCluster
		cluster    *clusterv1.Cluster
		elfMachine *infrav1.ElfMachine
		machine    *clusterv1.Machine
		secret     *corev1.Secret
		logBuffer  *bytes.Buffer
	)

	BeforeEach(func() {
		logBuffer = new(bytes.Buffer)
		klog.SetOutput(logBuffer)

		elfCluster, cluster, elfMachine, machine, secret = fake.NewClusterAndMachineObjects()
	})

	AfterEach(func() {
	})

	Context("Reconcile a ElfMachineTemplate", func() {
		It("Reconcile", func() {
			emt := fake.NewElfMachineTemplate()
			emt.OwnerReferences = append(emt.OwnerReferences, metav1.OwnerReference{Kind: fake.ClusterKind, APIVersion: clusterv1.GroupVersion.String(), Name: cluster.Name, UID: "blah"})
			kcp := fake.NewKCP()
			kcp.Spec.MachineTemplate = controlplanev1.KubeadmControlPlaneMachineTemplate{
				InfrastructureRef: corev1.ObjectReference{Namespace: emt.Namespace, Name: emt.Name},
			}
			md := fake.NewMD()
			md.Labels = map[string]string{clusterv1.ClusterNameLabel: cluster.Name}
			md.Spec.Template = clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					InfrastructureRef: corev1.ObjectReference{Namespace: emt.Namespace, Name: emt.Name},
				},
			}
			cluster.Spec.ControlPlaneRef = &corev1.ObjectReference{Namespace: kcp.Namespace, Name: kcp.Name}
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, emt, kcp, md)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			emtKey := capiutil.ObjectKey(emt)
			reconciler := &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: emtKey})
			Expect(result).To(BeZero())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring(fmt.Sprintf("ElfMachines resources of kcp %s are up to date", klog.KObj(kcp))))
			Expect(logBuffer.String()).To(ContainSubstring(fmt.Sprintf("ElfMachines resources of md %s are up to date", klog.KObj(md))))

			emt.Spec.Template.Spec.DiskGiB = 0
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, emt, kcp, md)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			emtKey = capiutil.ObjectKey(emt)
			reconciler = &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			result, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: emtKey})
			Expect(result).To(BeZero())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring(fmt.Sprintf("ElfMachines resources of kcp %s are up to date", klog.KObj(kcp))))
			Expect(logBuffer.String()).To(ContainSubstring(fmt.Sprintf("ElfMachines resources of md %s are up to date", klog.KObj(md))))
		})

		It("should not error and not requeue the request without elfmachinetemplate", func() {
			emt := fake.NewElfMachineTemplate()
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			reconciler := &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(emt)})
			Expect(result).To(BeZero())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("ElfMachineTemplate not found, won't reconcile"))

			emt.OwnerReferences = append(emt.OwnerReferences, metav1.OwnerReference{Kind: fake.ClusterKind, APIVersion: clusterv1.GroupVersion.String(), Name: cluster.Name, UID: "blah"})
			ctrlMgrCtx = fake.NewControllerManagerContext(cluster, elfMachine, machine, secret, emt)
			reconciler = &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			result, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(emt)})
			Expect(result).To(BeZero())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("ElfMachineTemplate Waiting for ElfCluster"))
		})

		It("should not error and not requeue the request when Cluster is paused", func() {
			emt := fake.NewElfMachineTemplate()
			emt.OwnerReferences = append(emt.OwnerReferences, metav1.OwnerReference{Kind: fake.ClusterKind, APIVersion: clusterv1.GroupVersion.String(), Name: cluster.Name, UID: "blah"})
			cluster.Spec.Paused = true
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, emt)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			emtKey := capiutil.ObjectKey(emt)
			reconciler := &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: emtKey})
			Expect(result).To(BeZero())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("ElfMachineTemplate linked to a cluster that is paused"))
		})
	})

	Context("reconcileWorkerResources", func() {
		It("reconcileWorkerResources", func() {
			emt := fake.NewElfMachineTemplate()
			md := fake.NewMD()
			md.Labels = map[string]string{clusterv1.ClusterNameLabel: cluster.Name}
			md.Spec.Replicas = ptr.To[int32](3)
			md.Spec.Template = clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					InfrastructureRef: corev1.ObjectReference{Namespace: emt.Namespace, Name: emt.Name},
				},
			}
			md.Spec.Strategy = &clusterv1.MachineDeploymentStrategy{
				RollingUpdate: &clusterv1.MachineRollingUpdateDeployment{
					MaxSurge:       intOrStrPtr(1),
					MaxUnavailable: intOrStrPtr(1),
				},
			}
			fake.ToWorkerMachine(elfMachine, md)
			fake.ToWorkerMachine(machine, md)
			fake.SetElfMachineTemplateForElfMachine(elfMachine, emt)
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			mtCtx := newMachineTemplateContext(elfCluster, cluster, emt)
			reconciler := &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			ok, err := reconciler.reconcileWorkerResources(ctx, mtCtx)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring(fmt.Sprintf("ElfMachines resources of md %s are up to date", klog.KObj(md))))

			logBuffer.Reset()
			elfMachine.Spec.DiskGiB -= 1
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, md)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			mtCtx = newMachineTemplateContext(elfCluster, cluster, emt)
			reconciler = &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			ok, err = reconciler.reconcileWorkerResources(ctx, mtCtx)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).NotTo(ContainSubstring("Resources of ElfMachine is not up to date, marking for resources not up to date and waiting for hot updating resources"))
			Expect(logBuffer.String()).To(ContainSubstring("Resources of ElfMachine is not up to date, marking for updating resources"))
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for worker ElfMachines to be updated resources"))

			// logBuffer.Reset()
			// elfMachine.Spec.DiskGiB -= 1
			// updatingElfMachine, updatingMachine := fake.NewMachineObjects(elfCluster, cluster)
			// fake.ToWorkerMachine(updatingElfMachine, md)
			// fake.ToWorkerMachine(updatingMachine, md)
			// fake.SetElfMachineTemplateForElfMachine(updatingElfMachine, emt)
			// ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, md, updatingElfMachine, updatingMachine)
			// fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			// fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, updatingElfMachine, updatingMachine)
			// mtCtx = newMachineTemplateContext(elfCluster, cluster, emt)
			// reconciler = &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			// ok, err = reconciler.reconcileWorkerResources(ctx, mtCtx)
			// Expect(logBuffer.String()).To(ContainSubstring("Resources of ElfMachine is not up to date, marking for updating resources"))
		})

		It("selectToBeUpdatedAndNeedUpdatedElfMachines", func() {
			elfMachine1, _ := fake.NewMachineObjects(elfCluster, cluster)
			elfMachine2, _ := fake.NewMachineObjects(elfCluster, cluster)

			toBeUpdated, needUpdated := selectToBeUpdatedAndNeedUpdatedElfMachines(false, 1, []*infrav1.ElfMachine{}, []*infrav1.ElfMachine{elfMachine1, elfMachine2})
			Expect(toBeUpdated).To(BeEmpty())
			Expect(needUpdated).To(Equal([]*infrav1.ElfMachine{elfMachine1, elfMachine2}))

			toBeUpdated, needUpdated = selectToBeUpdatedAndNeedUpdatedElfMachines(true, 1, []*infrav1.ElfMachine{elfMachine1}, []*infrav1.ElfMachine{elfMachine2})
			Expect(toBeUpdated).To(BeEmpty())
			Expect(needUpdated).To(Equal([]*infrav1.ElfMachine{elfMachine2}))

			toBeUpdated, needUpdated = selectToBeUpdatedAndNeedUpdatedElfMachines(true, 2, []*infrav1.ElfMachine{elfMachine1}, []*infrav1.ElfMachine{elfMachine2})
			Expect(toBeUpdated).To(Equal([]*infrav1.ElfMachine{elfMachine2}))
			Expect(needUpdated).To(BeEmpty())

			toBeUpdated, needUpdated = selectToBeUpdatedAndNeedUpdatedElfMachines(true, 1, []*infrav1.ElfMachine{}, []*infrav1.ElfMachine{elfMachine1, elfMachine2})
			Expect(toBeUpdated).To(Equal([]*infrav1.ElfMachine{elfMachine1}))
			Expect(needUpdated).To(Equal([]*infrav1.ElfMachine{elfMachine2}))
		})
	})

	Context("reconcileCPResources", func() {
		It("reconcileCPResources", func() {
			emt := fake.NewElfMachineTemplate()
			kcp := fake.NewKCP()
			kcp.Spec.MachineTemplate = controlplanev1.KubeadmControlPlaneMachineTemplate{
				InfrastructureRef: corev1.ObjectReference{Namespace: emt.Namespace, Name: "notfoud"},
			}
			cluster.Spec.ControlPlaneRef = &corev1.ObjectReference{Namespace: kcp.Namespace, Name: kcp.Name}
			elfMachine.Spec.DiskGiB -= 1
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kcp)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			mtCtx := newMachineTemplateContext(elfCluster, cluster, emt)
			reconciler := &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			ok, err := reconciler.reconcileCPResources(ctx, mtCtx)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())

			kcp.Spec.MachineTemplate = controlplanev1.KubeadmControlPlaneMachineTemplate{
				InfrastructureRef: corev1.ObjectReference{Namespace: emt.Namespace, Name: emt.Name},
			}
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kcp)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			mtCtx = newMachineTemplateContext(elfCluster, cluster, emt)
			reconciler = &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			ok, err = reconciler.reconcileCPResources(ctx, mtCtx)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())

			logBuffer.Reset()
			updatingElfMachine, updatingMachine := fake.NewMachineObjects(elfCluster, cluster)
			fake.ToCPMachine(updatingElfMachine, kcp)
			fake.ToCPMachine(updatingMachine, kcp)
			fake.SetElfMachineTemplateForElfMachine(updatingElfMachine, emt)
			conditions.MarkFalse(updatingElfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.WaitingForResourcesHotUpdateReason, clusterv1.ConditionSeverityInfo, "")
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kcp,
				updatingElfMachine, updatingMachine,
			)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, updatingElfMachine, updatingMachine)
			mtCtx = newMachineTemplateContext(elfCluster, cluster, emt)
			reconciler = &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			ok, err = reconciler.reconcileCPResources(ctx, mtCtx)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for control plane ElfMachines to be updated resources"))

			logBuffer.Reset()
			kcp.Spec.Replicas = ptr.To[int32](3)
			kcp.Status.Replicas = 3
			kcp.Status.UpdatedReplicas = 2
			fake.ToCPMachine(elfMachine, kcp)
			fake.ToCPMachine(machine, kcp)
			elfMachine.Spec.DiskGiB -= 1
			machine.Status.NodeRef = &corev1.ObjectReference{}
			conditions.MarkTrue(machine, controlplanev1.MachineAPIServerPodHealthyCondition)
			conditions.MarkTrue(machine, controlplanev1.MachineControllerManagerPodHealthyCondition)
			conditions.MarkTrue(machine, controlplanev1.MachineSchedulerPodHealthyCondition)
			conditions.MarkTrue(machine, controlplanev1.MachineEtcdPodHealthyCondition)
			conditions.MarkTrue(machine, controlplanev1.MachineEtcdMemberHealthyCondition)
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kcp)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			mtCtx = newMachineTemplateContext(elfCluster, cluster, emt)
			reconciler = &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			ok, err = reconciler.reconcileCPResources(ctx, mtCtx)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("KCP rolling update in progress, skip updating resources"))
			Expect(logBuffer.String()).NotTo(ContainSubstring("Resources of ElfMachine is not up to date, marking for updating resources"))

			logBuffer.Reset()
			kcp.Status.UpdatedReplicas = 3
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret, kcp)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			mtCtx = newMachineTemplateContext(elfCluster, cluster, emt)
			reconciler = &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			ok, err = reconciler.reconcileCPResources(ctx, mtCtx)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Resources of ElfMachine is not up to date, marking for updating resources"))
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for control plane ElfMachines to be updated resources"))
		})
	})

	Context("preflightChecksForCP", func() {
		It("should return false if KCP rolling update in progress", func() {
			emt := fake.NewElfMachineTemplate()
			kcp := fake.NewKCP()
			kcp.Spec.Replicas = ptr.To[int32](3)
			kcp.Status.Replicas = 3
			kcp.Status.UpdatedReplicas = 2
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			mtCtx := newMachineTemplateContext(elfCluster, cluster, emt)
			reconciler := &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			ok, err := reconciler.preflightChecksForCP(ctx, mtCtx, kcp)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("KCP rolling update in progress, skip updating resources"))
		})

		It("should return false if has deleting or failed machine", func() {
			emt := fake.NewElfMachineTemplate()
			kcp := fake.NewKCP()
			kcp.Spec.Replicas = ptr.To[int32](3)
			kcp.Status.Replicas = 3
			kcp.Status.UpdatedReplicas = 3
			fake.ToCPMachine(elfMachine, kcp)
			fake.ToCPMachine(machine, kcp)
			ctrlutil.AddFinalizer(machine, infrav1.MachineFinalizer)
			machine.DeletionTimestamp = &metav1.Time{Time: time.Now().UTC()}
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			mtCtx := newMachineTemplateContext(elfCluster, cluster, emt)
			reconciler := &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			ok, err := reconciler.preflightChecksForCP(ctx, mtCtx, kcp)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for machines to be deleted"))

			logBuffer.Reset()
			machine.DeletionTimestamp = nil
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			reconciler = &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			ok, err = reconciler.preflightChecksForCP(ctx, mtCtx, kcp)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for control plane to pass preflight checks"))

			logBuffer.Reset()
			machine.Status.NodeRef = &corev1.ObjectReference{}
			conditions.MarkFalse(machine, controlplanev1.MachineEtcdPodHealthyCondition, controlplanev1.PodInspectionFailedReason, clusterv1.ConditionSeverityInfo, "error")
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			reconciler = &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			ok, err = reconciler.preflightChecksForCP(ctx, mtCtx, kcp)
			Expect(ok).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for control plane to pass preflight checks"))
		})

		It("should return true", func() {
			emt := fake.NewElfMachineTemplate()
			kcp := fake.NewKCP()
			kcp.Spec.Replicas = ptr.To[int32](3)
			kcp.Status.Replicas = 3
			kcp.Status.UpdatedReplicas = 3
			fake.ToCPMachine(elfMachine, kcp)
			fake.ToCPMachine(machine, kcp)
			machine.Status.NodeRef = &corev1.ObjectReference{}
			conditions.MarkTrue(machine, controlplanev1.MachineAPIServerPodHealthyCondition)
			conditions.MarkTrue(machine, controlplanev1.MachineControllerManagerPodHealthyCondition)
			conditions.MarkTrue(machine, controlplanev1.MachineSchedulerPodHealthyCondition)
			conditions.MarkTrue(machine, controlplanev1.MachineEtcdPodHealthyCondition)
			conditions.MarkTrue(machine, controlplanev1.MachineEtcdMemberHealthyCondition)
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			mtCtx := newMachineTemplateContext(elfCluster, cluster, emt)
			reconciler := &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			ok, err := reconciler.preflightChecksForCP(ctx, mtCtx, kcp)
			Expect(ok).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("preflightChecksForWorker", func() {
		It("should return false if MD rolling update in progress", func() {
			md := fake.NewMD()
			fake.ToWorkerMachine(elfMachine, md)
			fake.ToWorkerMachine(machine, md)
			md.Spec.Replicas = ptr.To[int32](3)
			md.Status.Replicas = 3
			md.Status.UpdatedReplicas = 2
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			reconciler := &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			ok := reconciler.preflightChecksForWorker(ctx, md, nil)
			Expect(ok).To(BeFalse())
			Expect(logBuffer.String()).To(ContainSubstring("MD rolling update in progress, skip updating resources"))
		})

		It("should check maxSurge", func() {
			md := fake.NewMD()
			fake.ToWorkerMachine(elfMachine, md)
			fake.ToWorkerMachine(machine, md)
			md.Spec.Strategy = &clusterv1.MachineDeploymentStrategy{
				RollingUpdate: &clusterv1.MachineRollingUpdateDeployment{MaxSurge: intOrStrPtr(1)},
			}
			md.Spec.Replicas = ptr.To[int32](3)
			md.Status.Replicas = 3
			md.Status.UpdatedReplicas = 3
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			reconciler := &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			ok := reconciler.preflightChecksForWorker(ctx, md, []*infrav1.ElfMachine{elfMachine})
			Expect(ok).To(BeFalse())
			Expect(logBuffer.String()).To(ContainSubstring("Hot updated worker ElfMachine has reached the max number of concurrencies, so waiting for worker ElfMachines to be updated resources"))

			logBuffer.Reset()
			md.Status.UnavailableReplicas = 3
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			reconciler = &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			ok = reconciler.preflightChecksForWorker(ctx, md, []*infrav1.ElfMachine{})
			Expect(ok).To(BeFalse())
			Expect(logBuffer.String()).To(ContainSubstring("MD unavailable replicas"))

			md.Status.UnavailableReplicas = 0
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			reconciler = &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			ok = reconciler.preflightChecksForWorker(ctx, md, []*infrav1.ElfMachine{})
			Expect(ok).To(BeTrue())
		})
	})

	Context("selectResourcesNotUpToDateElfMachines", func() {
		It("should return updating/needUpdated resources elfMachines", func() {
			emt := fake.NewElfMachineTemplate()
			upToDateElfMachine, upToDateMachine := fake.NewMachineObjects(elfCluster, cluster)
			fake.SetElfMachineTemplateForElfMachine(upToDateElfMachine, emt)
			noUpToDateElfMachine, noUpToDateMachine := fake.NewMachineObjects(elfCluster, cluster)
			fake.SetElfMachineTemplateForElfMachine(noUpToDateElfMachine, emt)
			noUpToDateElfMachine.Spec.DiskGiB -= 1
			updatingElfMachine, updatingMachine := fake.NewMachineObjects(elfCluster, cluster)
			fake.SetElfMachineTemplateForElfMachine(updatingElfMachine, emt)
			conditions.MarkFalse(updatingElfMachine, infrav1.ResourcesHotUpdatedCondition, infrav1.WaitingForResourcesHotUpdateReason, clusterv1.ConditionSeverityInfo, "")
			failedElfMachine, failedMachine := fake.NewMachineObjects(elfCluster, cluster)
			fake.SetElfMachineTemplateForElfMachine(failedElfMachine, emt)
			failedElfMachine.Spec.DiskGiB -= 1
			failedMachine.Status.Phase = string(clusterv1.MachinePhaseFailed)
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret,
				upToDateElfMachine, upToDateMachine,
				noUpToDateElfMachine, noUpToDateMachine,
				updatingElfMachine, updatingMachine,
				failedElfMachine, failedMachine,
			)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, upToDateElfMachine, upToDateMachine)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, noUpToDateElfMachine, noUpToDateMachine)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, updatingElfMachine, updatingMachine)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, failedElfMachine, failedMachine)
			reconciler := &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			elfMachines := []*infrav1.ElfMachine{upToDateElfMachine, noUpToDateElfMachine, updatingElfMachine, failedElfMachine}
			updatingResourcesElfMachines, needUpdatedResourcesElfMachines, err := reconciler.selectResourcesNotUpToDateElfMachines(ctx, emt, elfMachines)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatingResourcesElfMachines).To(Equal([]*infrav1.ElfMachine{updatingElfMachine}))
			Expect(needUpdatedResourcesElfMachines).To(Equal([]*infrav1.ElfMachine{noUpToDateElfMachine}))
		})
	})

	Context("markElfMachinesToBeUpdatedResources", func() {
		It("should mark resources to be updated", func() {
			emt := fake.NewElfMachineTemplate()
			fake.SetElfMachineTemplateForElfMachine(elfMachine, emt)
			elfMachine.Spec.DiskGiB -= 1
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			reconciler := &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			err := reconciler.markElfMachinesToBeUpdatedResources(ctx, emt, []*infrav1.ElfMachine{elfMachine})
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Resources of ElfMachine is not up to date, marking for updating resources"))
			elfMachineKey := client.ObjectKey{Namespace: elfMachine.Namespace, Name: elfMachine.Name}
			Eventually(func() bool {
				_ = reconciler.Client.Get(ctx, elfMachineKey, elfMachine)
				return elfMachine.Spec.DiskGiB == emt.Spec.Template.Spec.DiskGiB
			}, timeout).Should(BeTrue())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForResourcesHotUpdateReason}})
		})
	})

	Context("markElfMachinesResourcesNotUpToDate", func() {
		It("should mark resources not up to date", func() {
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			emt := fake.NewElfMachineTemplate()
			fake.SetElfMachineTemplateForElfMachine(elfMachine, emt)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			reconciler := &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			err := reconciler.markElfMachinesResourcesNotUpToDate(ctx, emt, []*infrav1.ElfMachine{elfMachine})
			Expect(err).NotTo(HaveOccurred())
			expectConditions(elfMachine, []conditionAssertion{})

			logBuffer.Reset()
			elfMachine.Spec.DiskGiB -= 1
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, secret)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			reconciler = &ElfMachineTemplateReconciler{ControllerManagerContext: ctrlMgrCtx}
			err = reconciler.markElfMachinesResourcesNotUpToDate(ctx, emt, []*infrav1.ElfMachine{elfMachine})
			Expect(err).NotTo(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Resources of ElfMachine is not up to date, marking for resources not up to date and waiting for hot updating resources"))
			elfMachineKey := client.ObjectKey{Namespace: elfMachine.Namespace, Name: elfMachine.Name}
			Eventually(func() bool {
				_ = reconciler.Client.Get(ctx, elfMachineKey, elfMachine)
				return elfMachine.Spec.DiskGiB == emt.Spec.Template.Spec.DiskGiB
			}, timeout).Should(BeTrue())
			expectConditions(elfMachine, []conditionAssertion{{infrav1.ResourcesHotUpdatedCondition, corev1.ConditionFalse, clusterv1.ConditionSeverityInfo, infrav1.WaitingForResourcesHotUpdateReason}})
			Expect(conditions.GetMessage(elfMachine, infrav1.ResourcesHotUpdatedCondition)).To(Equal(anotherMachineHotUpdateInProgressMessage))
		})
	})
})
