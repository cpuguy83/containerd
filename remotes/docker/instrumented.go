package docker

import (
	"context"

	"github.com/containerd/containerd/tracing"
	"github.com/docker/go-metrics"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type instrumentedResolver struct {
	r dockerResolver

	resolveCounter metrics.LabeledCounter
}

func (r *instrumentedResolver) Resolve(ctx context.Context, ref string) (string, v1.Descriptor, error) {
	span, ctx := tracing.StartSpan(ctx, "docker.Resolve")
	defer span.End()

	span.SetAttributes(attribute.String("ref", ref))

	s, desc, err := r.r.Resolve(ctx, ref)
	if errors.Is(err, ErrInvalidAuthorization) {
		span.SetStatus(codes.Error, err.Error())
	}
	return s, desc, err
}
