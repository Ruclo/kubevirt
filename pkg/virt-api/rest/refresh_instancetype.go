package rest

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/emicklei/go-restful/v3"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/log"

	"kubevirt.io/kubevirt/pkg/apimachinery/patch"
)

func (app *SubresourceAPIApp) RefreshInstancetypeRequestHandler(request *restful.Request, response *restful.Response) {
	name := request.PathParameter("name")
	namespace := request.PathParameter("namespace")

	vm, statusErr := app.fetchVirtualMachine(name, namespace)
	if statusErr != nil {
		writeError(statusErr, response)
		return
	}

	// Check if VM has an instancetype or preference configured
	if vm.Spec.Instancetype == nil {
		writeError(errors.NewConflict(
			v1.Resource("virtualmachine"),
			name,
			fmt.Errorf("VirtualMachine does not have an instancetype configured"),
		), response)
		return
	}

	// Create patch to set refresh flags
	patchSet := patch.New()
	if vm.Status.InstancetypeRef == nil {
		writeError(errors.NewConflict(
			v1.Resource("virtualmachine"),
			name,
			fmt.Errorf("Instancetype controller revision has not been created yet"),
		), response)
		return
	}

	if vm.Status.InstancetypeRef.Refresh {
		// Already refreshing
		writeError(errors.NewConflict(
			v1.Resource("virtualmachine"),
			name,
			fmt.Errorf("Instancetype refresh already pending"),
		), response)
		return
	}

	patchSet.AddOption(patch.WithReplace("/status/instancetypeRef/refresh", true))
	patchBytes, err := patchSet.GeneratePayload()
	if err != nil {
		writeError(errors.NewInternalError(err), response)
		return
	}

	log.Log.Object(vm).V(4).Infof("Patching VM status for instancetype refresh: %s", string(patchBytes))
	_, patchErr := app.virtCli.VirtualMachine(vm.Namespace).PatchStatus(
		context.Background(),
		vm.Name,
		types.JSONPatchType,
		patchBytes,
		//TODO: DryRun
		metav1.PatchOptions{},
	)

	if patchErr != nil {
		if strings.Contains(patchErr.Error(), jsonpatchTestErr) {
			writeError(errors.NewConflict(v1.Resource("virtualmachine"), name, patchErr), response)
		} else {
			writeError(errors.NewInternalError(patchErr), response)
		}
		return
	}

	response.WriteHeader(http.StatusAccepted)
}
