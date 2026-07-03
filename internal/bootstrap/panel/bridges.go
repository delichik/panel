package panel

import (
	"context"
	"errors"

	"panel/internal/modules/applications"
	"panel/internal/modules/containers"
	"panel/internal/modules/facilityapps"
)

type applicationCertificateBridge struct {
	apps *applications.Service
}

func (b *applicationCertificateBridge) RedeployChangedApplications(ctx context.Context) (int, error) {
	if b.apps == nil {
		return 0, errors.New("application service is not initialized")
	}
	return b.apps.RedeployChangedApplications(ctx)
}

func (b *applicationCertificateBridge) RedeployEnabledApplications(ctx context.Context) (int, error) {
	if b.apps == nil {
		return 0, errors.New("application service is not initialized")
	}
	return b.apps.RedeployEnabledApplications(ctx)
}

func (b *applicationCertificateBridge) ReconcileReverseProxy(ctx context.Context) error {
	if b.apps == nil {
		return errors.New("application service is not initialized")
	}
	return b.apps.ReconcileReverseProxy(ctx)
}

type applicationContainerBridge struct {
	apps       *applications.Service
	containers *containerization.Service
	facility   *facilityapps.Service
}

func (b *applicationContainerBridge) Execute(ctx context.Context, serverID string, run func(context.Context) error) error {
	if b.containers == nil {
		return errors.New("container service is not initialized")
	}
	return b.containers.Execute(ctx, serverID, run)
}

func (b *applicationContainerBridge) List(ctx context.Context) ([]applications.Application, error) {
	if b.apps == nil {
		return nil, errors.New("application service is not initialized")
	}
	return b.apps.List(ctx)
}

func (b *applicationContainerBridge) UpdateImage(ctx context.Context, id string) (applications.OperationResult, error) {
	if b.apps == nil {
		return applications.OperationResult{}, errors.New("application service is not initialized")
	}
	return b.apps.UpdateImage(ctx, id)
}

func (b *applicationContainerBridge) Deploy(ctx context.Context, id string) (applications.OperationResult, error) {
	if b.apps == nil {
		return applications.OperationResult{}, errors.New("application service is not initialized")
	}
	return b.apps.Deploy(ctx, id)
}

func (b *applicationContainerBridge) PlanApplicationDeployment(ctx context.Context, req applications.DeploymentPlanRequest) (applications.DeploymentPlanResult, error) {
	if b.apps == nil {
		return applications.DeploymentPlanResult{}, errors.New("application service is not initialized")
	}
	return b.apps.PlanApplicationDeployment(ctx, req)
}
