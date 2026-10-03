package catalog

import (
	"context"
	"fmt"
	"strings"
)

func (a *Server) ListChannelModels(ctx context.Context, ownerUserID, channelID int) (ListResponse[ChannelModel], error) {
	return a.store.ListChannelModels(ctx, ownerUserID, channelID)
}

func (a *Server) CreateChannelModel(ctx context.Context, ownerUserID, channelID int, in ChannelModel) (ChannelModel, error) {
	if _, err := a.store.GetChannelDTO(ctx, ownerUserID, channelID); err != nil {
		return ChannelModel{}, err
	}
	if strings.TrimSpace(in.ModelName) == "" {
		return ChannelModel{}, fmt.Errorf("%w: model_name is required", ErrInvalid)
	}
	if strings.TrimSpace(in.UpstreamModel) == "" {
		return ChannelModel{}, fmt.Errorf("%w: upstream_model is required", ErrInvalid)
	}
	// The same channel cannot expose two mappings under one public model name.
	exists, err := a.store.ChannelModelExists(ctx, ownerUserID, channelID, in.ModelName)
	if err != nil {
		return ChannelModel{}, err
	}
	if exists {
		return ChannelModel{}, fmt.Errorf("%w: model mapping already exists", ErrInvalid)
	}
	return a.store.InsertChannelModel(ctx, channelID, in)
}

// UpdateChannelModel updates the public model name and enabled flag. The
// upstream (real) model name is upstream-owned and cannot change, so pricing
// (keyed by upstream) is unaffected by a rename.
func (a *Server) UpdateChannelModel(ctx context.Context, ownerUserID, channelID, modelID int, modelName string, enabled bool) (ChannelModel, error) {
	if _, err := a.store.GetChannelDTO(ctx, ownerUserID, channelID); err != nil {
		return ChannelModel{}, err
	}
	current, ok, err := a.store.GetChannelModelByID(ctx, channelID, modelID)
	if err != nil {
		return ChannelModel{}, err
	}
	if !ok {
		return ChannelModel{}, ErrNotFound
	}
	newName := strings.TrimSpace(modelName)
	if newName == "" {
		newName = current.ModelName
	}
	if newName != current.ModelName {
		exists, err := a.store.ChannelModelExists(ctx, ownerUserID, channelID, newName)
		if err != nil {
			return ChannelModel{}, err
		}
		if exists {
			return ChannelModel{}, fmt.Errorf("%w: model mapping already exists", ErrInvalid)
		}
	}
	var updated ChannelModel
	err = a.tx.InTx(ctx, func(tx Tx) error {
		model, ok, err := tx.UpdateChannelModelRecord(channelID, modelID, newName, enabled)
		if err != nil {
			return err
		}
		if !ok {
			return ErrNotFound
		}
		updated = model
		return nil
	})
	if err != nil {
		return ChannelModel{}, err
	}
	return updated, nil
}

func (a *Server) DeleteChannelModel(ctx context.Context, ownerUserID, channelID, modelID int) error {
	if _, err := a.store.GetChannelDTO(ctx, ownerUserID, channelID); err != nil {
		return err
	}
	ok, err := a.store.DeleteChannelModel(ctx, channelID, modelID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

func (a *Server) ListCatalogModels(ctx context.Context, ownerUserID int, enabledOnly bool) (ListResponse[CatalogModelDTO], error) {
	return a.store.ListCatalogModels(ctx, ownerUserID, enabledOnly)
}

func (a *Server) RouteCandidate(ctx context.Context, ownerUserID int, modelName string, channelID int) (RouteCandidate, bool, error) {
	return a.store.RouteCandidate(ctx, ownerUserID, modelName, channelID, int(a.baseBreakerFor(ctx, ownerUserID).Cooldown.Seconds()))
}

func (a *Server) RouteCandidates(ctx context.Context, ownerUserID int, modelName string) (ListResponse[RouteCandidate], error) {
	return a.store.RouteCandidates(ctx, ownerUserID, modelName, int(a.baseBreakerFor(ctx, ownerUserID).Cooldown.Seconds()))
}
