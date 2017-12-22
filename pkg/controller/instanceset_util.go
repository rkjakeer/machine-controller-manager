/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

This file was copied and modified from the kubernetes/kubernetes project
https://github.com/kubernetes/kubernetes/blob/release-1.8/pkg/controller/deployment/util/replicaset_util.go

Modifications Copyright 2017 The Gardener Authors.
*/
package controller

import (
	"fmt"

	"github.com/golang/glog"

	errorsutil "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/retry"
	labelsutil "k8s.io/kubernetes/pkg/util/labels"

	"code.sapcloud.io/kubernetes/node-controller-manager/pkg/apis/node/v1alpha1"
	v1alpha1client "code.sapcloud.io/kubernetes/node-controller-manager/pkg/client/clientset/typed/node/v1alpha1"
	v1alpha1listers "code.sapcloud.io/kubernetes/node-controller-manager/pkg/client/listers/node/v1alpha1"

)

// TODO: use client library instead when it starts to support update retries
//       see https://github.com/kubernetes/kubernetes/issues/21479
type updateISFunc func(is *v1alpha1.InstanceSet) error

// UpdateISWithRetries updates a RS with given applyUpdate function. Note that RS not found error is ignored.
// The returned bool value can be used to tell if the RS is actually updated.
func UpdateISWithRetries(isClient v1alpha1client.InstanceSetInterface, isLister v1alpha1listers.InstanceSetLister, namespace, name string, applyUpdate updateISFunc) (*v1alpha1.InstanceSet, error) {
	var is *v1alpha1.InstanceSet

	retryErr := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var err error
		is, err = isLister.Get(name)
		if err != nil {
			return err
		}
		is = is.DeepCopy()
		// Apply the update, then attempt to push it to the apiserver.
		if applyErr := applyUpdate(is); applyErr != nil {
			return applyErr
		}
		is, err = isClient.Update(is)
		return err
	})

	// Ignore the precondition violated error, but the RS isn't updated.
	if retryErr == errorsutil.ErrPreconditionViolated {
		glog.V(4).Infof("Instance set %s precondition doesn't hold, skip updating it.", name)
		retryErr = nil
	}

	return is, retryErr
}

// TODO : Redefine ?
func GetInstanceSetHash(is *v1alpha1.InstanceSet, uniquifier *int32) (string, error) {
	isTemplate := is.Spec.Template.DeepCopy()
	isTemplate.Labels = labelsutil.CloneAndRemoveLabel(isTemplate.Labels, v1alpha1.DefaultInstanceDeploymentUniqueLabelKey)
	return fmt.Sprintf("%d", ComputeHash(isTemplate, uniquifier)), nil
}
