package server

import (
	"github.com/containerd/containerd/errdefs"
	"golang.org/x/net/context"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"
)

func (c *criService) PodSandboxStats(ctx context.Context, req *runtime.PodSandboxStatsRequest) (*runtime.PodSandboxStatsResponse, error) {
	sb, err := c.sandboxStore.Get(req.GetPodSandboxId())
	if err != nil {
		return nil, errdefs.ToGRPC(err)
	}

	attrs := runtime.PodSandboxAttributes{
		Id: sb.ID,
	}

	stats := runtime.PodSandboxStatsResponse{
		Stats: &runtime.PodSandboxStats{},
	}
	if sb.CNIResult != nil {
		resp.Stats = &runtime.NetworkStats{}
		for name, cfg := range sb.CNIResult.Interfaces {
			resp.Stats.Network.Interfaces = append(resp.Stats.Network.Interfaces, &runtime.InterfaceStats{})
		}
	}
	return nil, errdefs.ToGRPC(errdefs.ErrNotImplemented)
}
