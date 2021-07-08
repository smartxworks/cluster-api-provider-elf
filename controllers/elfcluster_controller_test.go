package controllers

import (
	"bytes"
	goctx "context"
	"flag"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	infrav1 "github.com/smartxworks/cluster-api-provider-elf/api/v1alpha4"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	"github.com/smartxworks/cluster-api-provider-elf/test/fake"
)

const (
	Timeout = time.Second * 30
)

var _ = Describe("ElfClusterReconciler", func() {
	var (
		elfCluster *infrav1.ElfCluster
		cluster    *clusterv1.Cluster
	)

	BeforeEach(func() {
		// set log
		if err := flag.Set("logtostderr", "false"); err != nil {
			_ = fmt.Errorf("Error setting logtostderr flag")
		}
		if err := flag.Set("v", "6"); err != nil {
			_ = fmt.Errorf("Error setting v flag")
		}
		klog.SetOutput(GinkgoWriter)

		elfCluster, cluster = fake.NewClusterObjects()
	})

	Context("Reconcile an ElfCluster", func() {
		It("should not error and not requeue the request without cluster", func() {
			ctrlMgrContext := fake.NewControllerManagerContext(elfCluster)
			ctrlContext := &context.ControllerContext{
				ControllerManagerContext: ctrlMgrContext,
				Logger:                   log.Log,
			}

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfClusterReconciler{ctrlContext}

			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: util.ObjectKey(elfCluster)})
			Expect(err).To(BeNil())
			Expect(result.RequeueAfter).To(BeZero())
			Expect(buf.String()).To(ContainSubstring("Waiting for Cluster Controller to set OwnerRef on ElfCluster"))
		})

		It("should not error and not requeue the request when Cluster is paused", func() {
			cluster.Spec.Paused = true

			ctrlMgrContext := fake.NewControllerManagerContext(cluster, elfCluster)
			ctrlContext := &context.ControllerContext{
				ControllerManagerContext: ctrlMgrContext,
				Logger:                   ctrlMgrContext.Logger,
			}

			fake.InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfClusterReconciler{ctrlContext}
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: util.ObjectKey(elfCluster)})
			Expect(err).To(BeNil())
			Expect(result.RequeueAfter).To(BeZero())
			Expect(buf.String()).To(ContainSubstring("ElfCluster linked to a cluster that is paused"))
		})

		It("should add finalizer to the elfcluster", func() {
			ctrlMgrContext := fake.NewControllerManagerContext(cluster, elfCluster)
			ctrlContext := &context.ControllerContext{
				ControllerManagerContext: ctrlMgrContext,
				Logger:                   log.Log,
			}

			fake.InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)

			elfClusterKey := util.ObjectKey(elfCluster)
			reconciler := &ElfClusterReconciler{ctrlContext}
			_, _ = reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfClusterKey})
			elfCluster = &infrav1.ElfCluster{}
			Expect(reconciler.Client.Get(reconciler, elfClusterKey, elfCluster)).To(Succeed())
			Expect(elfCluster.Status.Ready).To(BeTrue())
			Expect(elfCluster.Finalizers).To(ContainElement(infrav1.ClusterFinalizer))
		})

		It("should error if without ControlPlaneEndpoint", func() {
			ctrlMgrContext := fake.NewControllerManagerContext(cluster, elfCluster)
			ctrlContext := &context.ControllerContext{
				ControllerManagerContext: ctrlMgrContext,
				Logger:                   log.Log,
			}

			fake.InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ctrlContext}
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: util.ObjectKey(elfCluster)})
			Expect(err.Error()).To(ContainSubstring("Failed to reconcile ControlPlaneEndpoint for ElfCluster"))
			Expect(result).To(BeZero())
		})
	})

	Context("Delete a ElfCluster", func() {
		BeforeEach(func() {
			ctrlutil.AddFinalizer(elfCluster, infrav1.ClusterFinalizer)
			elfCluster.DeletionTimestamp = &metav1.Time{Time: time.Now().UTC()}
		})

		It("should not remove elfcluster finalizer when has elfmachines", func() {
			elfMachine, machine := fake.NewMachineObjects(elfCluster, cluster)

			ctrlMgrContext := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine)
			ctrlContext := &context.ControllerContext{
				ControllerManagerContext: ctrlMgrContext,
				Logger:                   log.Log,
			}

			fake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)

			buf := new(bytes.Buffer)
			klog.SetOutput(buf)

			reconciler := &ElfClusterReconciler{ctrlContext}

			elfClusterKey := util.ObjectKey(elfCluster)
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfClusterKey})
			Expect(buf.String()).To(ContainSubstring("Waiting for ElfMachines to be deleted"))
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(err).Should(BeNil())
			elfCluster = &infrav1.ElfCluster{}
			Expect(reconciler.Client.Get(reconciler, elfClusterKey, elfCluster)).To(Succeed())
			Expect(elfCluster.Finalizers).To(ContainElement(infrav1.ClusterFinalizer))
		})

		It("should remove elfcluster finalizer", func() {
			ctrlMgrContext := fake.NewControllerManagerContext(cluster, elfCluster)
			ctrlContext := &context.ControllerContext{
				ControllerManagerContext: ctrlMgrContext,
				Logger:                   log.Log,
			}

			fake.InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ctrlContext}

			elfClusterKey := util.ObjectKey(elfCluster)
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: elfClusterKey})
			Expect(result).To(BeZero())
			Expect(err).To(BeZero())
			elfCluster = &infrav1.ElfCluster{}
			err = reconciler.Client.Get(reconciler, elfClusterKey, elfCluster)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
	})
})
