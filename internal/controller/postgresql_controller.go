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
	"fmt"
	"sort"

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
//+kubebuilder:rbac:groups=*,resources=pods,verbs=get;list

const (
	podOwnerKey = ".metadata.controller"
)

func (r *PostgresqlReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Get the current state in the cluster
	var postgresPods core.PodList
	if err := r.List(ctx, &postgresPods, client.InNamespace(req.Namespace), client.MatchingFields{podOwnerKey: req.Name}); err != nil {
		log.Error(err, "could not list postgres pods")
		return ctrl.Result{}, err
	}
	sort.Slice(postgresPods.Items, func(i, j int) bool {
		return postgresPods.Items[i].Name < postgresPods.Items[j].Name
	})

	// Get the custom resource object from the cluster
	var postgres postgresv1.Postgresql
	if err := r.Get(ctx, req.NamespacedName, &postgres); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Update the state
	if len(postgresPods.Items) < postgres.Spec.Replicas {
		// Create pods
		for i := len(postgresPods.Items); i < postgres.Spec.Replicas; i++ {
			name := fmt.Sprintf("%s-%d", postgres.Name, i)
			pod := createPostgresPod(name, postgres.Namespace)

			if err := ctrl.SetControllerReference(&postgres, pod, r.Scheme); err != nil {
				log.Error(err, "could not set controller reference")
				return ctrl.Result{}, err
			}

			if err := r.Create(ctx, pod); err != nil {
				log.Error(err, "could not create postgres pod", "pod", pod)
				return ctrl.Result{}, err
			}
		}
	} else if len(postgresPods.Items) > postgres.Spec.Replicas {
		// Delete pods
		for i := len(postgresPods.Items) - 1; i >= postgres.Spec.Replicas; i-- {
			pod := postgresPods.Items[i]
			if err := r.Delete(ctx, &pod); client.IgnoreNotFound(err) != nil {
				log.Error(err, "could not delete postgres pod", "pod", pod)
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

func createPostgresPod(name string, namespace string) *core.Pod {
	return &core.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: core.PodSpec{
			Containers: []core.Container{
				{
					Name:  name,
					Image: "postgres:15",
					EnvFrom: []core.EnvFromSource{
						{
							SecretRef: &core.SecretEnvSource{
								LocalObjectReference: core.LocalObjectReference{
									Name: "postgres-credentials",
								},
							},
						},
					},
				},
			},
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *PostgresqlReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &core.Pod{}, podOwnerKey, func(rawObj client.Object) []string {
		job := rawObj.(*core.Pod)
		owner := metav1.GetControllerOf(job)
		if owner == nil {
			return nil
		}
		if owner.Kind != "Postgresql" {
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
