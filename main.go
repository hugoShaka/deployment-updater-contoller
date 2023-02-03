package main

import (
	"context"
	"flag"
	"github.com/google/go-github/v50/github"
	"github.com/gravitational/trace"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
	runtimescheme "sigs.k8s.io/controller-runtime/pkg/scheme"
	"sync"
	"time"
)

var (
	SchemeBuilder = &runtimescheme.Builder{GroupVersion: appsv1.SchemeGroupVersion}
	scheme        = runtime.NewScheme()
)

const (
	namespace   = "namespace"
	releaseName = "release"
)

func init() {
	SchemeBuilder.Register(
		&appsv1.Deployment{},
		&appsv1.DeploymentList{},
		&appsv1.StatefulSet{},
		&appsv1.StatefulSetList{},
	)
	utilruntime.Must(SchemeBuilder.AddToScheme(scheme))
}

type statefulsetVersionReconciler struct {
	kclient.Client
	Scheme  *runtime.Scheme
	vClient versionClient
}

type versionClient struct {
	mu         sync.Mutex
	version    string
	validUntil time.Time
}

func (v *versionClient) Version(ctx context.Context) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.version != "" && v.validUntil.After(time.Now()) {
		return v.version, nil
	}

	client := github.NewClient(nil)
	release, _, err := client.Repositories.GetLatestRelease(ctx, "gravitational", "teleport")
	v.validUntil = time.Now().Add(time.Hour)
	if err != nil {
		return "", err
	}
	v.version = *release.TagName
	return v.version, nil
}

func (r *statefulsetVersionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var obj appsv1.StatefulSet
	if err := r.Get(ctx, req.NamespacedName, &obj); err != nil {
		if apierrors.IsNotFound(err) {
			// log.Info("not found")
			return ctrl.Result{}, nil
		}
		// log.Error(err, "failed to get resource")
		return ctrl.Result{}, trace.Wrap(err)
	}

	// getVersion retrieves the version from internet, validates signature and implement a basic cache
	obj.Spec.Template.Spec.Containers[0].Image, _ = r.vClient.Version(ctx)
	err := r.Update(ctx, &obj)
	return ctrl.Result{}, trace.Wrap(err)
}

func (r *statefulsetVersionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.StatefulSet{}).
		Complete(r)
}

type deploymentVersionReconciler struct {
	kclient.Client
	Scheme  *runtime.Scheme
	vClient versionClient
}

func (r *deploymentVersionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var obj appsv1.Deployment
	if err := r.Get(ctx, req.NamespacedName, &obj); err != nil {
		if apierrors.IsNotFound(err) {
			// log.Info("not found")
			return ctrl.Result{}, nil
		}
		// log.Error(err, "failed to get resource")
		return ctrl.Result{}, trace.Wrap(err)
	}

	// getVersion retrieves the version from internet, validates signature and implement a basic cache
	obj.Spec.Template.Spec.Containers[0].Image, _ = r.vClient.Version(ctx)
	err := r.Update(ctx, &obj)
	return ctrl.Result{}, trace.Wrap(err)
}

func (r *deploymentVersionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.Deployment{}).
		Complete(r)
}

func main() {
	ctx := ctrl.SetupSignalHandler()

	var metricsAddr string
	var probeAddr string
	var leaderElectionID string
	var syncPeriodString string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&leaderElectionID, "leader-election-id", "431e83f4.teleport.dev", "Leader Election Id to use")
	flag.StringVar(&syncPeriodString, "sync-period", "10h", "Operator sync period (format: https://pkg.go.dev/time#ParseDuration)")

	syncPeriod, _ := time.ParseDuration(syncPeriodString)
	mgr, _ := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         true,
		LeaderElectionID:       leaderElectionID,
		Namespace:              namespace,
		SyncPeriod:             &syncPeriod,
		NewCache: cache.BuilderWithOptions(cache.Options{
			SelectorsByObject: cache.SelectorsByObject{
				&appsv1.Deployment{}: {
					Field: fields.SelectorFromSet(fields.Set{"metadata.name": releaseName}),
				},
				&appsv1.StatefulSet{}: {
					Field: fields.SelectorFromSet(fields.Set{"metadata.name": releaseName}),
				},
			},
		}),
	})

	_ = (&statefulsetVersionReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr)

	_ = (&deploymentVersionReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr)

	if err := mgr.Start(ctx); err != nil {
		os.Exit(1)
	}

}
