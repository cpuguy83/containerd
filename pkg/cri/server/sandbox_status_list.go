package server

import (
	"github.com/containerd/containerd/errdefs"
	"golang.org/x/net/context"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"
)

func (c *criService) ListPodSandboxStats(context.Context, *runtime.ListPodSandboxStatsRequest) (*runtime.ListPodSandboxStatsResponse, error) {
	return nil, errdefs.ToGRPC(errdefs.ErrNotImplemented)
}
