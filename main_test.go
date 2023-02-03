package main

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corescheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"testing"
	"time"
)

func setupTest(t *testing.T) kclient.Client {

	testEnv := &envtest.Environment{}
	cfg, err := testEnv.Start()

	require.NoError(t, err)
	require.NotNil(t, cfg)

	k8sClient, err := kclient.New(cfg, kclient.Options{Scheme: corescheme.Scheme})
	require.NoError(t, err)
	ns := &core.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}

	ctx := context.Background()

	err = k8sClient.Create(ctx, ns)
	require.NoError(t, err)

	t.Cleanup(func() {
		err = testEnv.Stop()
		require.NoError(t, err)
	})

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: "0",
	})
	require.NoError(t, err)

	err = (&statefulsetVersionReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr)
	require.NoError(t, err)

	err = (&deploymentVersionReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr)
	require.NoError(t, err)

	go func() {
		err := mgr.Start(ctx)
		assert.NoError(t, err)
	}()
	return k8sClient
}

func intPtr(i int32) *int32 {
	return &i
}

func TestDeployment(t *testing.T) {
	ctx := context.Background()
	k8sClient := setupTest(t)
	obj := &v1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      releaseName,
			Namespace: namespace,
		},
		Spec: v1.DeploymentSpec{
			Replicas: intPtr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"foo": "bar",
				},
				MatchExpressions: nil,
			},
			Template: core.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"foo": "bar",
					},
					Annotations:     nil,
					OwnerReferences: nil,
					Finalizers:      nil,
					ManagedFields:   nil,
				},
				Spec: core.PodSpec{
					Containers: []core.Container{
						{
							Name:    "teleport",
							Image:   "changeme",
							Command: []string{"foo"},
						},
					},
				},
			},
		},
		Status: v1.DeploymentStatus{},
	}
	err := k8sClient.Create(ctx, obj)
	require.NoError(t, err)

	time.Sleep(5 * time.Second)

	err = k8sClient.Get(ctx, kclient.ObjectKey{
		Namespace: namespace,
		Name:      releaseName,
	}, obj)
	require.NoError(t, err)

	t.Logf("image: %s", obj.Spec.Template.Spec.Containers[0].Image)
}
