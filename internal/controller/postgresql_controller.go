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

package controller

import (
	"context"

	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	postgresv1 "flohansen/postgres-operator/api/v1"
)

// PostgresqlReconciler reconciles a Postgresql object
type PostgresqlReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=postgres.flohansen,resources=postgresqls,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=postgres.flohansen,resources=postgresqls/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=postgres.flohansen,resources=postgresqls/finalizers,verbs=update

const (
	podOwnerKey = ".metadata.controller"
)

func (r *PostgresqlReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var postgresPods core.PodList
	if err := r.List(ctx, &postgresPods, client.InNamespace(req.Namespace), client.MatchingFields{podOwnerKey: req.Name}); err != nil {
		log.Error(err, "could not list postgres pods")
		return ctrl.Result{}, err
	}

	var postgres postgresv1.Postgresql
	if err := r.Get(ctx, req.NamespacedName, &postgres); err != nil {
		return r.cleanupPods(ctx, &postgresPods)
	}

	if len(postgresPods.Items) == postgres.Spec.Replicas {
		return ctrl.Result{}, nil
	}

	// TODO: Add / Remove / Update postgres pods

	return ctrl.Result{}, nil
}

func (r *PostgresqlReconciler) cleanupPods(ctx context.Context, pods *core.PodList) (ctrl.Result, error) {
	if len(pods.Items) == 0 {
		return ctrl.Result{}, nil
	}

	for _, item := range pods.Items {
		if err := r.Delete(ctx, &item); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PostgresqlReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &core.Pod{}, podOwnerKey, func(rawObject client.Object) []string {
		pod := rawObject.(*core.Pod)

		owner := metav1.GetControllerOf(pod)
		if owner == nil {
			return nil
		}

		if owner.APIVersion != core.SchemeGroupVersion.String() || owner.Kind != "Postgres" {
			return nil
		}

		return []string{owner.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&postgresv1.Postgresql{}).
		Owns(&core.Pod{}).
		Complete(r)
}
