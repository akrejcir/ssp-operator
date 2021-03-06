package common

import (
	"fmt"
	"reflect"

	"github.com/go-logr/logr"

	libhandler "github.com/operator-framework/operator-lib/handler"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type StatusMessage = *string

type ResourceStatus struct {
	Resource     controllerutil.Object
	Progressing  StatusMessage
	NotAvailable StatusMessage
	Degraded     StatusMessage
}

type ReconcileFunc = func(*Request) (ResourceStatus, error)

func CollectResourceStatus(request *Request, funcs ...ReconcileFunc) ([]ResourceStatus, error) {
	res := make([]ResourceStatus, 0, len(funcs))
	for _, f := range funcs {
		status, err := f(request)
		if err != nil {
			return nil, err
		}
		res = append(res, status)
	}
	return res, nil
}

type ResourceUpdateFunc = func(expected, found controllerutil.Object)
type ResourceStatusFunc = func(resource controllerutil.Object) ResourceStatus

func CreateOrUpdateResource(request *Request, resource controllerutil.Object, updateResource ResourceUpdateFunc) (ResourceStatus, error) {
	return createOrUpdate(request, resource, false, updateResource, statusOk)
}

func CreateOrUpdateResourceWithStatus(request *Request, resource controllerutil.Object, updateResource ResourceUpdateFunc, statusFunc ResourceStatusFunc) (ResourceStatus, error) {
	return createOrUpdate(request, resource, false, updateResource, statusFunc)
}

func CreateOrUpdateClusterResource(request *Request, resource controllerutil.Object, updateResource ResourceUpdateFunc) (ResourceStatus, error) {
	return createOrUpdate(request, resource, true, updateResource, statusOk)
}

func CreateOrUpdateClusterResourceWithStatus(request *Request, resource controllerutil.Object, updateResource ResourceUpdateFunc, statusFunc ResourceStatusFunc) (ResourceStatus, error) {
	return createOrUpdate(request, resource, true, updateResource, statusFunc)
}

func statusOk(_ controllerutil.Object) ResourceStatus {
	return ResourceStatus{}
}

func createOrUpdate(request *Request, resource controllerutil.Object, isClusterRes bool, updateResource ResourceUpdateFunc, statusFunc ResourceStatusFunc) (ResourceStatus, error) {
	err := setOwner(request, resource, isClusterRes)
	if err != nil {
		return ResourceStatus{}, err
	}

	found := newEmptyResource(resource)
	found.SetName(resource.GetName())
	found.SetNamespace(resource.GetNamespace())
	res, err := controllerutil.CreateOrUpdate(request.Context, request.Client, found, func() error {
		if request.ResourceVersionCache.Contains(found) {
			return nil
		}

		// We expect users will not add any other owner references,
		// if that is not correct, this code needs to be changed.
		found.SetOwnerReferences(resource.GetOwnerReferences())

		updateLabels(resource, found)
		updateAnnotations(resource, found)
		updateResource(resource, found)
		return nil
	})
	if err != nil {
		return ResourceStatus{}, err
	}

	request.ResourceVersionCache.Add(found)
	logOperation(res, found, request.Logger)
	return operationStatus(res, found, statusFunc), nil
}

func setOwner(request *Request, resource controllerutil.Object, isClusterRes bool) error {
	if isClusterRes {
		return libhandler.SetOwnerAnnotations(request.Instance, resource)
	} else {
		return controllerutil.SetControllerReference(request.Instance, resource, request.Scheme)
	}
}

func newEmptyResource(resource controllerutil.Object) controllerutil.Object {
	return reflect.New(reflect.TypeOf(resource).Elem()).Interface().(controllerutil.Object)
}

func updateAnnotations(expected, found controllerutil.Object) {
	if found.GetAnnotations() == nil {
		found.SetAnnotations(expected.GetAnnotations())
		return
	}
	updateStringMap(expected.GetAnnotations(), found.GetAnnotations())
}

func updateLabels(expected, found controllerutil.Object) {
	if found.GetLabels() == nil {
		found.SetLabels(expected.GetLabels())
		return
	}
	updateStringMap(expected.GetLabels(), found.GetLabels())
}

func updateStringMap(expected, found map[string]string) {
	if expected == nil {
		return
	}
	for key, val := range expected {
		found[key] = val
	}
}

func logOperation(result controllerutil.OperationResult, resource controllerutil.Object, logger logr.Logger) {
	if result == controllerutil.OperationResultCreated {
		logger.Info(fmt.Sprintf("Created %s resource: %s",
			resource.GetObjectKind().GroupVersionKind().Kind,
			resource.GetName()))
	} else if result == controllerutil.OperationResultUpdated {
		logger.Info(fmt.Sprintf("Updated %s resource: %s",
			resource.GetObjectKind().GroupVersionKind().Kind,
			resource.GetName()))
	}
}

func operationStatus(result controllerutil.OperationResult, resource controllerutil.Object, statusFunc ResourceStatusFunc) ResourceStatus {
	switch result {
	case controllerutil.OperationResultCreated:
		msg := "Creating resource."
		return ResourceStatus{
			Resource:     resource,
			Progressing:  &msg,
			NotAvailable: &msg,
			Degraded:     &msg,
		}
	case controllerutil.OperationResultUpdated:
		msg := "Updating resource."
		return ResourceStatus{
			Resource:     resource,
			Progressing:  &msg,
			NotAvailable: &msg,
			Degraded:     &msg,
		}
	default:
		status := statusFunc(resource)
		status.Resource = resource
		return status
	}
}
