package catalog

import "context"

func (a *Server) ListChannelModels(ctx context.Context, ownerUserID, channelID int) (ListResponse[ChannelModel], error) {
	return a.store.ListChannelModels(ctx, ownerUserID, channelID)
}

func (a *Server) CreateChannelModel(ctx context.Context, ownerUserID, channelID int, in ChannelModel) (ChannelModel, error) {
	if _, err := a.store.GetChannelDTO(ctx, ownerUserID, channelID); err != nil {
		return ChannelModel{}, err
	}
	return a.store.InsertChannelModel(ctx, channelID, in)
}

func (a *Server) UpdateChannelModel(ctx context.Context, ownerUserID, channelID, modelID int, upstreamModel string, enabled bool) (ChannelModel, error) {
	if _, err := a.store.GetChannelDTO(ctx, ownerUserID, channelID); err != nil {
		return ChannelModel{}, err
	}
	model, ok, err := a.store.UpdateChannelModelRecord(ctx, channelID, modelID, upstreamModel, enabled)
	if err != nil {
		return ChannelModel{}, err
	}
	if !ok {
		return ChannelModel{}, ErrNotFound
	}
	return model, nil
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

func (a *Server) RouteCandidates(ctx context.Context, ownerUserID int, modelName string) (ListResponse[RouteCandidate], error) {
	return a.store.RouteCandidates(ctx, ownerUserID, modelName, int(a.baseBreakerFor(ctx, ownerUserID).Cooldown.Seconds()))
}
